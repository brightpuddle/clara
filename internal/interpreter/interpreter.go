// Package interpreter implements the Clara state machine execution engine.
// It traverses an Intent graph, calls registered tools via the Registry,
// accumulates results in a mem map, and evaluates transition conditions using
// expr-lang/expr.
package interpreter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"text/template"

	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/cockroachdb/errors"
	"github.com/expr-lang/expr"
	"github.com/rs/zerolog"
)

// WaitFunc is called when the interpreter reaches a state with no immediate
// next-state resolution (e.g. a PROMPT_USER state). The implementation should
// block until external input is available and return the result to merge into mem.
// A nil WaitFunc causes the interpreter to return an error when a Wait is needed.
type WaitFunc func(ctx context.Context, stateName string, mem map[string]any) (any, error)

// StateChangeFunc is an optional callback invoked after each state transition.
// Useful for persisting run state for crash recovery.
type StateChangeFunc func(ctx context.Context, runID, intentID, stateName string, mem map[string]any)

type StepEvent struct {
	RunID    string
	IntentID string
	State    string
	Action   string
	Args     any
	Result   any
	Error    string
}

type StepFunc func(ctx context.Context, event StepEvent)

// Interpreter executes Intent state machines.
type Interpreter struct {
	reg      *registry.Registry
	log      zerolog.Logger
	wait     WaitFunc
	onChange StateChangeFunc
	onStep   StepFunc
}

// New creates an Interpreter backed by the provided Registry.
func New(reg *registry.Registry, log zerolog.Logger) *Interpreter {
	return &Interpreter{
		reg: reg,
		log: log.With().Str("component", "interpreter").Logger(),
	}
}

// WithWait sets the WaitFunc called when a state requires external input.
func (it *Interpreter) WithWait(fn WaitFunc) *Interpreter {
	it.wait = fn
	return it
}

// WithOnChange sets a callback invoked after each successful state transition.
func (it *Interpreter) WithOnChange(fn StateChangeFunc) *Interpreter {
	it.onChange = fn
	return it
}

func (it *Interpreter) WithOnStep(fn StepFunc) *Interpreter {
	it.onStep = fn
	return it
}

// RunOptions configures a single intent execution.
type RunOptions struct {
	// RunID is a unique identifier for this execution (used for state persistence).
	RunID string
	// InitialMem optionally pre-seeds the mem map (e.g. for resuming a run).
	InitialMem map[string]any
	// Entrypoint is the name of the Starlark function to call (defaults to "main").
	Entrypoint string
	// HandlerArgs are the arguments to pass to the entrypoint function.
	HandlerArgs any
}

// Execute runs intent starting from startState.
// It returns nil when a terminal state is reached, or an error if execution
// cannot proceed.
func (it *Interpreter) Execute(
	ctx context.Context,
	intent *orchestrator.Intent,
	startState string,
	opts RunOptions,
) error {
	log := it.log.With().
		Str("intent_id", intent.ID).
		Str("run_id", opts.RunID).
		Logger()

	// mem accumulates the results of each state's action for use in
	// templates and transition conditions.
	mem := make(map[string]any)
	if opts.InitialMem != nil {
		for k, v := range opts.InitialMem {
			mem[k] = v
		}
	}

	currentState := startState

	for {
		select {
		case <-ctx.Done():
			return errors.Wrap(ctx.Err(), "interpreter context cancelled")
		default:
		}

		state, ok := intent.States[currentState]
		if !ok {
			return errors.Newf("state %q not found in intent %q", currentState, intent.ID)
		}

		log.Debug().Str("state", currentState).Msg("entering state")

		// Execute the action if one is specified.
		if state.Action != "" {
			result, traceArgs, err := it.executeAction(ctx, state, mem, log)
			if err != nil {
				it.emitStep(ctx, StepEvent{
					RunID:    opts.RunID,
					IntentID: intent.ID,
					State:    currentState,
					Action:   state.Action,
					Args:     traceArgs,
					Error:    err.Error(),
				})
				return errors.Wrapf(err, "state %q action %q failed", currentState, state.Action)
			}
			mem[currentState] = result
			it.emitStep(ctx, StepEvent{
				RunID:    opts.RunID,
				IntentID: intent.ID,
				State:    currentState,
				Action:   state.Action,
				Args:     traceArgs,
				Result:   result,
			})
		} else {
			it.emitStep(ctx, StepEvent{
				RunID:    opts.RunID,
				IntentID: intent.ID,
				State:    currentState,
			})
		}

		if it.onChange != nil && opts.RunID != "" {
			it.onChange(ctx, opts.RunID, intent.ID, currentState, mem)
		}

		// Terminal state: we're done.
		if state.Terminal {
			log.Info().Str("state", currentState).Msg("reached terminal state")
			return nil
		}

		// Determine the next state.
		next, err := it.resolveNext(ctx, currentState, state, mem, opts, log)
		if err != nil {
			return err
		}

		currentState = next
	}
}

// executeAction resolves the tool from the registry, injects template args,
// and calls the tool.
func (it *Interpreter) executeAction(
	ctx context.Context,
	state orchestrator.State,
	mem map[string]any,
	log zerolog.Logger,
) (any, any, error) {
	tool, ok := it.reg.Get(state.Action)
	if !ok {
		return nil, nil, errors.Newf("tool %q not found in registry", state.Action)
	}

	if state.ForEach != "" {
		return it.executeForEach(ctx, state, mem, tool, log)
	}

	injectedArgs, err := injectTemplates(state.Args, mem)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "inject templates for action %q", state.Action)
	}

	log.Debug().Str("action", state.Action).Msg("calling tool")
	result, err := tool(ctx, injectedArgs)
	if err != nil {
		return nil, injectedArgs, errors.Wrapf(err, "call tool %q", state.Action)
	}
	return result, injectedArgs, nil
}

func (it *Interpreter) executeForEach(
	ctx context.Context,
	state orchestrator.State,
	mem map[string]any,
	tool registry.Tool,
	log zerolog.Logger,
) (any, any, error) {
	items, err := evalExpression(state.ForEach, mem)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "evaluate for_each for action %q", state.Action)
	}

	collection, err := toAnySlice(items)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "for_each for action %q", state.Action)
	}

	itemName := state.Item
	if itemName == "" {
		itemName = "item"
	}

	results := make([]any, 0, len(collection))
	traceCalls := make([]map[string]any, 0, len(collection))
	for idx, item := range collection {
		loopMem := cloneMem(mem)
		loopMem[itemName] = item
		loopMem[itemName+"_index"] = idx

		injectedArgs, err := injectTemplates(state.Args, loopMem)
		if err != nil {
			return nil, traceCalls, errors.Wrapf(
				err,
				"inject foreach args for action %q at index %d",
				state.Action,
				idx,
			)
		}

		log.Debug().
			Str("action", state.Action).
			Int("foreach_index", idx).
			Msg("calling foreach action")
		result, err := tool(ctx, injectedArgs)
		if err != nil {
			traceCalls = append(traceCalls, map[string]any{
				"index": idx,
				"item":  item,
				"args":  injectedArgs,
			})
			return nil, traceCalls, errors.Wrapf(
				err,
				"call foreach action %q at index %d",
				state.Action,
				idx,
			)
		}
		traceCalls = append(traceCalls, map[string]any{
			"index":  idx,
			"item":   item,
			"args":   injectedArgs,
			"result": result,
		})
		results = append(results, map[string]any{
			"item":   item,
			"result": result,
			"index":  idx,
		})
	}
	return results, map[string]any{"for_each": state.ForEach, "calls": traceCalls}, nil
}

// resolveNext determines the next state using transition conditions and the
// default Next field. If no next state can be resolved and a WaitFunc is
// configured, it calls Wait to obtain external input before re-evaluating.
func (it *Interpreter) resolveNext(
	ctx context.Context,
	stateName string,
	state orchestrator.State,
	mem map[string]any,
	opts RunOptions,
	log zerolog.Logger,
) (string, error) {
	// Evaluate transitions in order.
	for _, t := range state.Transitions {
		matched, err := evalCondition(t.Condition, mem)
		if err != nil {
			log.Warn().
				Str("state", stateName).
				Str("condition", t.Condition).
				Err(err).
				Msg("condition evaluation error; skipping transition")
			continue
		}
		if matched {
			log.Debug().
				Str("state", stateName).
				Str("next", t.Next).
				Str("condition", t.Condition).
				Msg("transition matched")
			return t.Next, nil
		}
	}

	// Use the default next state if set.
	if state.Next != "" {
		return state.Next, nil
	}

	// No next state: enter Wait if a WaitFunc is configured.
	if it.wait != nil {
		log.Info().Str("state", stateName).Msg("waiting for external input")
		input, err := it.wait(ctx, stateName, mem)
		if err != nil {
			return "", errors.Wrapf(err, "wait at state %q", stateName)
		}
		mem[stateName] = input

		// Re-evaluate transitions after receiving input.
		for _, t := range state.Transitions {
			matched, err := evalCondition(t.Condition, mem)
			if err != nil {
				continue
			}
			if matched {
				return t.Next, nil
			}
		}
	}

	return "", errors.Newf("dead end: no next state from %q", stateName)
}

// evalCondition evaluates an expr-lang condition string against the mem map.
// Returns (false, nil) if the condition evaluates to a non-bool.
func evalCondition(condition string, mem map[string]any) (bool, error) {
	out, err := evalExpression(condition, mem)
	if err != nil {
		return false, errors.Wrapf(err, "eval condition %q", condition)
	}
	result, ok := out.(bool)
	if !ok {
		return false, errors.Newf("condition %q did not return bool (got %T)", condition, out)
	}
	return result, nil
}

func evalExpression(expression string, mem map[string]any) (any, error) {
	out, err := expr.Eval(expression, mem)
	if err != nil {
		return nil, errors.Wrapf(err, "eval expression %q", expression)
	}
	return out, nil
}

// injectTemplates resolves {{handlebars}}-style template expressions in the
// args map, replacing them with values from mem.
func injectTemplates(args map[string]any, mem map[string]any) (map[string]any, error) {
	if args == nil {
		return nil, nil
	}
	result := make(map[string]any, len(args))
	for k, v := range args {
		injected, err := injectValue(v, mem)
		if err != nil {
			return nil, errors.Wrapf(err, "inject arg %q", k)
		}
		result[k] = injected
	}
	return result, nil
}

func injectValue(v any, mem map[string]any) (any, error) {
	switch val := v.(type) {
	case string:
		return renderTemplate(val, mem)
	case map[string]any:
		if exprValue, ok, err := exprArgValue(val, mem); ok || err != nil {
			return exprValue, err
		}
		return injectTemplates(val, mem)
	case []any:
		result := make([]any, len(val))
		for i, item := range val {
			injected, err := injectValue(item, mem)
			if err != nil {
				return nil, err
			}
			result[i] = injected
		}
		return result, nil
	default:
		return v, nil
	}
}

func exprArgValue(val map[string]any, mem map[string]any) (any, bool, error) {
	if len(val) != 1 {
		return nil, false, nil
	}
	expression, ok := val["$expr"]
	if !ok {
		return nil, false, nil
	}
	exprString, ok := expression.(string)
	if !ok || exprString == "" {
		return nil, true, errors.New("$expr must be a non-empty string")
	}
	result, err := evalExpression(exprString, mem)
	if err != nil {
		return nil, true, err
	}
	return result, true, nil
}

// templateCache caches compiled Go templates to avoid recompilation.
var (
	templateCacheMu sync.RWMutex
	templateCache   = map[string]*template.Template{}
)

func renderTemplate(s string, mem map[string]any) (string, error) {
	// Fast path: no template markers.
	if !containsTemplate(s) {
		return s, nil
	}

	templateCacheMu.RLock()
	tmpl, ok := templateCache[s]
	templateCacheMu.RUnlock()

	if !ok {
		var err error
		tmpl, err = template.New("").
			Delims("{{", "}}").
			Parse(s)
		if err != nil {
			return "", errors.Wrapf(err, "parse template %q", s)
		}
		templateCacheMu.Lock()
		templateCache[s] = tmpl
		templateCacheMu.Unlock()
	}

	// Flatten mem to a map suitable for template execution.
	data := flattenForTemplate(mem)

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", errors.Wrapf(err, "execute template %q", s)
	}
	return buf.String(), nil
}

func containsTemplate(s string) bool {
	for i := 0; i < len(s)-1; i++ {
		if s[i] == '{' && s[i+1] == '{' {
			return true
		}
	}
	return false
}

// flattenForTemplate converts mem values to a form accessible in Go templates.
// JSON round-trips ensure consistent types regardless of how the tool stored results.
func flattenForTemplate(mem map[string]any) map[string]any {
	out := make(map[string]any, len(mem))
	for k, v := range mem {
		data, err := json.Marshal(v)
		if err != nil {
			out[k] = fmt.Sprintf("%v", v)
			continue
		}
		var decoded any
		if err := json.Unmarshal(data, &decoded); err != nil {
			out[k] = fmt.Sprintf("%v", v)
			continue
		}
		out[k] = decoded
	}
	return out
}

func cloneMem(mem map[string]any) map[string]any {
	out := make(map[string]any, len(mem))
	for k, v := range mem {
		out[k] = v
	}
	return out
}

func toAnySlice(value any) ([]any, error) {
	if value == nil {
		return nil, nil
	}
	if items, ok := value.([]any); ok {
		return items, nil
	}

	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, errors.Newf("expected slice or array, got %T", value)
	}

	items := make([]any, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		items[i] = rv.Index(i).Interface()
	}
	return items, nil
}

func (it *Interpreter) emitStep(ctx context.Context, event StepEvent) {
	if it.onStep == nil || event.RunID == "" {
		return
	}
	it.onStep(ctx, event)
}
