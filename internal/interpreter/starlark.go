package interpreter

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"
	"go.starlark.net/starlark"
)

const starlarkScriptState = "SCRIPT"

type ReplayEntry struct {
	Sequence   int
	RunID      string
	IntentID   string
	Entrypoint string
	Kind       string
	Name       string
	Args       any
	Result     any
	Error      string
}

type HistoryLoadFunc func(ctx context.Context, runID string) ([]ReplayEntry, error)
type HistoryAppendFunc func(ctx context.Context, runID, intentID string, entry ReplayEntry) error

type PauseRequest struct {
	Name string
	Args any
}

type PauseError struct {
	Request PauseRequest
}

func (e *PauseError) Error() string {
	return fmt.Sprintf("workflow paused waiting for %q", e.Request.Name)
}

type StarlarkInterpreter struct {
	reg           *registry.Registry
	log           zerolog.Logger
	wait          WaitFunc
	onChange      StateChangeFunc
	onStep        StepFunc
	loadHistory   HistoryLoadFunc
	appendHistory HistoryAppendFunc
	mcpTimeout    time.Duration
}

func NewStarlark(reg *registry.Registry, log zerolog.Logger) *StarlarkInterpreter {
	return &StarlarkInterpreter{
		reg: reg,
		log: log.With().Str("component", "starlark_interpreter").Logger(),
	}
}

func (it *StarlarkInterpreter) WithWait(fn WaitFunc) *StarlarkInterpreter {
	it.wait = fn
	return it
}

func (it *StarlarkInterpreter) WithOnChange(fn StateChangeFunc) *StarlarkInterpreter {
	it.onChange = fn
	return it
}

func (it *StarlarkInterpreter) WithOnStep(fn StepFunc) *StarlarkInterpreter {
	it.onStep = fn
	return it
}

func (it *StarlarkInterpreter) WithHistory(
	load HistoryLoadFunc,
	append HistoryAppendFunc,
) *StarlarkInterpreter {
	it.loadHistory = load
	it.appendHistory = append
	return it
}

func (it *StarlarkInterpreter) WithMCPTimeout(timeout time.Duration) *StarlarkInterpreter {
	it.mcpTimeout = timeout
	return it
}

func (it *StarlarkInterpreter) Execute(
	ctx context.Context,
	intent *orchestrator.Intent,
	_ string,
	opts RunOptions,
) error {
	if intent.Script == "" {
		return errors.New("starlark workflow requires script")
	}

	history := []ReplayEntry{}
	if it.loadHistory != nil && opts.RunID != "" {
		loaded, err := it.loadHistory(ctx, opts.RunID)
		if err != nil {
			return errors.Wrap(err, "load replay history")
		}
		history = loaded
	}

	if opts.RunID != "" {
		ctx = context.WithValue(ctx, orchestrator.ContextKeyRunID, opts.RunID)
		ctx = context.WithValue(ctx, orchestrator.ContextKeyIntentID, intent.ID)
	}

	runtime := &starlarkRuntime{
		ctx:           ctx,
		runID:         opts.RunID,
		intentID:      intent.ID,
		entrypoint:    opts.Entrypoint,
		reg:           it.reg,
		wait:          it.wait,
		onStep:        it.onStep,
		appendHistory: it.appendHistory,
		history:       history,
		log:           it.log.With().Str("intent_id", intent.ID).Str("run_id", opts.RunID).Logger(),
	}

	thread := &starlark.Thread{
		Name: intent.ID,
		Print: func(_ *starlark.Thread, msg string) {
			runtime.emitStep("print", nil, msg, "")
		},
	}

	predeclared := starlark.StringDict{
		"clara": &claraRuntimeBuiltins{rt: runtime},
		"tui":   &NamespaceProxy{rt: runtime, name: "tui"},
		"must":  orchestrator.MustModule,
	}

	for _, ns := range it.reg.Namespaces() {
		if ns == "clara" || ns == "tui" {
			continue // Protect core namespaces
		}
		predeclared[ns] = &NamespaceProxy{rt: runtime, name: ns}
	}

	globals, err := starlark.ExecFile(thread, intent.ID+".star", intent.Script, predeclared)
	if err != nil {
		var pauseErr *PauseError
		if errors.As(err, &pauseErr) {
			if it.onChange != nil && opts.RunID != "" {
				it.onChange(ctx, opts.RunID, intent.ID, starlarkScriptState, map[string]any{
					"wait": map[string]any{
						"name": pauseErr.Request.Name,
						"args": pauseErr.Request.Args,
					},
				})
			}
			return pauseErr
		}
		return errors.Wrap(err, "execute starlark workflow")
	}

	mainValue, ok := globals["main"]
	if opts.Entrypoint != "" {
		mainValue, ok = globals[opts.Entrypoint]
	}

	if !ok {
		if opts.Entrypoint != "" {
			return errors.Newf("starlark workflow entrypoint %q not found", opts.Entrypoint)
		}
		return errors.New("starlark workflow must define main()")
	}
	mainFn, ok := mainValue.(starlark.Callable)
	if !ok {
		if opts.Entrypoint != "" {
			return errors.Newf("starlark workflow entrypoint %q must be callable", opts.Entrypoint)
		}
		return errors.New("starlark workflow main must be callable")
	}

	var positional starlark.Tuple
	var kwargs []starlark.Tuple
	if opts.HandlerArgs != nil {
		// Handle typed nil maps (interface holding a nil map)
		val := reflect.ValueOf(opts.HandlerArgs)
		if val.Kind() == reflect.Map && val.IsNil() {
			opts.HandlerArgs = nil
		}
	}

	if opts.HandlerArgs != nil {
		if m, ok := opts.HandlerArgs.(map[string]any); ok {
			usePositional := false
			if fn, ok := mainFn.(*starlark.Function); ok {
				// Only auto-map to a single positional argument if the map is non-empty
				// and doesn't contain a key matching the parameter name.
				if fn.NumParams() == 1 && !fn.HasKwargs() && len(m) > 0 {
					p, _ := fn.Param(0)
					if _, ok := m[p]; !ok {
						usePositional = true
					}
				}
			}

			if usePositional {
				sv, err := orchestrator.GoToStarlark(m)
				if err != nil {
					return errors.Wrap(err, "prepare handler arguments")
				}
				positional = starlark.Tuple{sv}
			} else {
				for k, v := range m {
					if fn, ok := mainFn.(*starlark.Function); ok && !fn.HasKwargs() {
						expected := false
						for i := 0; i < fn.NumParams(); i++ {
							p, _ := fn.Param(i)
							if p == k {
								expected = true
								break
							}
						}
						if !expected {
							continue
						}
					}

					sv, err := orchestrator.GoToStarlark(v)
					if err != nil {
						return errors.Wrapf(err, "prepare handler argument %q", k)
					}
					kwargs = append(kwargs, starlark.Tuple{starlark.String(k), sv})
				}
			}
		} else {
			sv, err := orchestrator.GoToStarlark(opts.HandlerArgs)
			if err != nil {
				return errors.Wrap(err, "prepare handler arguments")
			}
			positional = starlark.Tuple{sv}
		}
	}

	mainResult, err := starlark.Call(thread, mainFn, positional, kwargs)
	if err != nil {
		var pauseErr *PauseError
		if errors.As(err, &pauseErr) {
			if it.onChange != nil && opts.RunID != "" {
				it.onChange(ctx, opts.RunID, intent.ID, starlarkScriptState, map[string]any{
					"wait": map[string]any{
						"name": pauseErr.Request.Name,
						"args": pauseErr.Request.Args,
					},
				})
			}
			return pauseErr
		}
		return errors.Wrap(err, "execute starlark main")
	}

	if runtime.cursor != len(runtime.history) {
		return errors.Newf(
			"starlark replay history has %d unused entrie(s)",
			len(runtime.history)-runtime.cursor,
		)
	}

	if it.onChange != nil && opts.RunID != "" {
		mem := globalsToGo(globals)
		if result, err := orchestrator.StarlarkValueToGo(mainResult); err == nil {
			mem["main_result"] = result
		}
		it.onChange(ctx, opts.RunID, intent.ID, starlarkScriptState, mem)
	}
	return nil
}

type starlarkRuntime struct {
	ctx           context.Context
	runID         string
	intentID      string
	entrypoint    string
	reg           *registry.Registry
	wait          WaitFunc
	onStep        StepFunc
	appendHistory HistoryAppendFunc
	history       []ReplayEntry
	cursor        int
	log           zerolog.Logger
}

func (rt *starlarkRuntime) noopBuiltin(
	_ *starlark.Thread,
	_ *starlark.Builtin,
	_ starlark.Tuple,
	_ []starlark.Tuple,
) (starlark.Value, error) {
	return starlark.None, nil
}

func (rt *starlarkRuntime) waitBuiltin(
	_ *starlark.Thread,
	_ *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	name, waitArgs, err := parseBuiltinCall(args, kwargs)
	if err != nil {
		return nil, err
	}
	result, err := rt.invoke("wait", name, waitArgs)
	if err != nil {
		return nil, err
	}
	return orchestrator.GoToStarlark(result)
}

func (rt *starlarkRuntime) searchBuiltin(
	_ *starlark.Thread,
	_ *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var query string
	var limit int
	if err := starlark.UnpackArgs("search", args, kwargs, "query", &query, "limit?", &limit); err != nil {
		return nil, err
	}

	results := make(map[string]any)
	searchArgs := map[string]any{"query": query}
	if limit > 0 {
		searchArgs["limit"] = limit
	}

	if zkResults, err := rt.invoke("tool", "zk.note_search", searchArgs); err == nil {
		results["zk"] = zkResults
	} else {
		results["zk_error"] = err.Error()
	}

	if webexResults, err := rt.invoke("tool", "webex.search_messages", searchArgs); err == nil {
		results["webex"] = webexResults
	} else {
		results["webex_error"] = err.Error()
	}

	if emailResults, err := rt.invoke("tool", "mail.search", searchArgs); err == nil {
		results["email"] = emailResults
	} else {
		results["email_error"] = err.Error()
	}

	return orchestrator.GoToStarlark(results)
}

func (rt *starlarkRuntime) invoke(kind, name string, args map[string]any) (any, error) {
	entry, replayed, err := rt.nextHistory(kind, name, args)
	if err != nil {
		return nil, err
	}
	if replayed {
		rt.emitStep(name, args, entry.Result, entry.Error)
		if entry.Error != "" {
			return nil, errors.New(entry.Error)
		}
		return entry.Result, nil
	}

	switch kind {
	case "tool":
		tool, ok := rt.reg.Get(name)
		if !ok {
			err := errors.Newf("tool %q not found in registry", name)
			if appendErr := rt.recordFresh(kind, name, args, nil, err); appendErr != nil {
				return nil, appendErr
			}
			rt.emitStep(name, args, nil, err.Error())
			return nil, err
		}
		result, err := tool(rt.ctx, args)
		result = registry.NormalizeToolResult(result)
		if appendErr := rt.recordFresh(kind, name, args, result, err); appendErr != nil {
			return nil, appendErr
		}
		if err != nil {
			rt.emitStep(name, args, nil, err.Error())
			return nil, err
		}
		rt.emitStep(name, args, result, "")
		return result, nil
	case "wait":
		if rt.wait == nil {
			return nil, &PauseError{Request: PauseRequest{Name: name, Args: args}}
		}
		result, err := rt.wait(rt.ctx, name, map[string]any{"args": args})
		if appendErr := rt.recordFresh(kind, name, args, result, err); appendErr != nil {
			return nil, appendErr
		}
		if err != nil {
			rt.emitStep("wait."+name, args, nil, err.Error())
			return nil, err
		}
		rt.emitStep("wait."+name, args, result, "")
		return result, nil
	default:
		return nil, errors.Newf("unsupported starlark call kind %q", kind)
	}
}

func (rt *starlarkRuntime) nextHistory(
	kind, name string,
	args map[string]any,
) (ReplayEntry, bool, error) {
	if rt.cursor >= len(rt.history) {
		return ReplayEntry{}, false, nil
	}

	entry := rt.history[rt.cursor]

	if entry.Kind != kind || entry.Name != name {
		return ReplayEntry{}, false, errors.Newf(
			"starlark replay divergence at sequence %d: expected %s %q, got %s %q",
			entry.Sequence, entry.Kind, entry.Name, kind, name,
		)
	}

	if name == "tui.notify.send_interactive" || name == "tui.notify.send" {
		matchArgs := cloneMap(args)
		delete(matchArgs, "id")
		delete(matchArgs, "run_id")
		delete(matchArgs, "intent_id")

		if entryArgs, ok := entry.Args.(map[string]any); ok {
			entryMatchArgs := cloneMap(entryArgs)
			delete(entryMatchArgs, "id")
			delete(entryMatchArgs, "run_id")
			delete(entryMatchArgs, "intent_id")

			if reflect.DeepEqual(entryMatchArgs, matchArgs) {
				rt.cursor++
				return entry, true, nil
			}
		}
	} else if reflect.DeepEqual(entry.Args, args) {
		rt.cursor++
		return entry, true, nil
	}

	return ReplayEntry{}, false, errors.Newf(
		"starlark replay divergence at sequence %d: %s %q args mismatch. expected %#v, got %#v",
		entry.Sequence,
		entry.Kind,
		entry.Name,
		entry.Args,
		args,
	)
}

func (rt *starlarkRuntime) recordFresh(
	kind, name string,
	args map[string]any,
	result any,
	callErr error,
) error {
	entry := ReplayEntry{
		Sequence:   rt.cursor,
		RunID:      rt.runID,
		IntentID:   rt.intentID,
		Entrypoint: rt.entrypoint,
		Kind:       kind,
		Name:       name,
		Args:       cloneMap(args),
		Result:     result,
	}
	if callErr != nil {
		entry.Error = callErr.Error()
	}
	if rt.appendHistory != nil && rt.runID != "" {
		if err := rt.appendHistory(rt.ctx, rt.runID, rt.intentID, entry); err != nil {
			return errors.Wrap(err, "append replay history")
		}
	}
	rt.history = append(rt.history, entry)
	rt.cursor++
	return nil
}

func (rt *starlarkRuntime) emitStep(action string, args, result any, errText string) {
	if rt.onStep == nil || rt.runID == "" {
		return
	}
	rt.onStep(rt.ctx, StepEvent{
		RunID:      rt.runID,
		IntentID:   rt.intentID,
		Entrypoint: rt.entrypoint,
		State:      starlarkScriptState,
		Action:     action,
		Args:       args,
		Result:     result,
		Error:      errText,
	})
}

func parseBuiltinCall(
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (string, map[string]any, error) {
	if len(args) == 0 {
		return "", nil, errors.New("first positional argument must be the tool or wait name")
	}
	name, ok := starlark.AsString(args[0])
	if !ok || name == "" {
		return "", nil, errors.New("tool or wait name must be a string")
	}

	callArgs := map[string]any{}
	if len(args) > 2 {
		return "", nil, errors.New("expected at most two positional arguments")
	}
	if len(args) == 2 {
		dict, ok := args[1].(*starlark.Dict)
		if !ok {
			return "", nil, errors.New("second positional argument must be a dict")
		}
		converted, err := orchestrator.StarlarkValueToGo(dict)
		if err != nil {
			return "", nil, err
		}
		callArgs, ok = converted.(map[string]any)
		if !ok {
			return "", nil, errors.New("dict arguments must convert to a string-keyed map")
		}
	}
	if len(kwargs) > 0 && len(args) == 2 {
		return "", nil, errors.New("cannot mix a dict positional argument with keyword arguments")
	}
	for _, kw := range kwargs {
		key, ok := starlark.AsString(kw.Index(0))
		if !ok {
			return "", nil, errors.New("keyword argument names must be strings")
		}
		value, err := orchestrator.StarlarkValueToGo(kw.Index(1))
		if err != nil {
			return "", nil, err
		}
		callArgs[key] = value
	}
	return name, callArgs, nil
}

func globalsToGo(globals starlark.StringDict) map[string]any {
	result := make(map[string]any, len(globals))
	for key, value := range globals {
		converted, err := orchestrator.StarlarkValueToGo(value)
		if err != nil {
			result[key] = value.String()
			continue
		}
		result[key] = converted
	}
	return result
}

func cloneMap(values map[string]any) map[string]any {
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

// NamespaceProxy exposes a registry namespace to Starlark as an object whose
// attributes are callable builtins that invoke registry tools.
type NamespaceProxy struct {
	rt   *starlarkRuntime
	name string
}

func (p *NamespaceProxy) String() string        { return fmt.Sprintf("<namespace %q>", p.name) }
func (p *NamespaceProxy) Type() string          { return "namespace" }
func (p *NamespaceProxy) Freeze()               {}
func (p *NamespaceProxy) Truth() starlark.Bool  { return true }
func (p *NamespaceProxy) Hash() (uint32, error) { return 0, fmt.Errorf("unhashable: %s", p.Type()) }

func (p *NamespaceProxy) Attr(name string) (starlark.Value, error) {
	// Sanitization: underscores in Starlark map to hyphens in tool names.
	sanitized := strings.ReplaceAll(name, "_", "-")
	fqName := p.name + "." + sanitized

	if !p.rt.reg.IsKnownNamespace(p.name) {
		return nil, nil
	}

	if p.rt.reg.IsKnownNamespace(fqName) {
		return &NamespaceProxy{rt: p.rt, name: fqName}, nil
	}

	return starlark.NewBuiltin(fqName, func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		actualFQName := fqName
		if !p.rt.reg.Has(actualFQName) {
			if p.rt.reg.Has(p.name + "." + name) {
				actualFQName = p.name + "." + name
			} else {
				return nil, errors.Newf("component %q is disconnected or has no tool %q", p.name, name)
			}
		}

		callArgs, err := parseKwargs(args, kwargs)
		if err != nil {
			return nil, errors.Wrapf(err, "parse arguments for %q", actualFQName)
		}
		result, err := p.rt.invoke("tool", actualFQName, callArgs)
		if err != nil {
			return nil, err
		}
		return orchestrator.GoToStarlark(result)
	}), nil
}

func (p *NamespaceProxy) AttrNames() []string {
	prefix := p.name + "."
	seen := make(map[string]struct{})
	for _, name := range p.rt.reg.Names() {
		if strings.HasPrefix(name, prefix) {
			action := strings.TrimPrefix(name, prefix)
			starlarkName := strings.ReplaceAll(action, "-", "_")
			seen[starlarkName] = struct{}{}
		}
	}

	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func parseKwargs(args starlark.Tuple, kwargs []starlark.Tuple) (map[string]any, error) {
	callArgs := map[string]any{}
	if len(args) > 1 {
		return nil, errors.New("expected at most one positional argument (a dict)")
	}
	if len(args) == 1 {
		dict, ok := args[0].(*starlark.Dict)
		if !ok {
			return nil, errors.New("positional argument must be a dict")
		}
		converted, err := orchestrator.StarlarkValueToGo(dict)
		if err != nil {
			return nil, err
		}
		var ok2 bool
		callArgs, ok2 = converted.(map[string]any)
		if !ok2 {
			return nil, errors.New("dict arguments must convert to a string-keyed map")
		}
	}
	if len(kwargs) > 0 && len(args) == 1 {
		return nil, errors.New("cannot mix a dict positional argument with keyword arguments")
	}
	for _, kw := range kwargs {
		key, ok := starlark.AsString(kw.Index(0))
		if !ok {
			return nil, errors.New("keyword argument names must be strings")
		}
		value, err := orchestrator.StarlarkValueToGo(kw.Index(1))
		if err != nil {
			return nil, err
		}
		callArgs[key] = value
	}
	return callArgs, nil
}

type claraRuntimeBuiltins struct {
	rt *starlarkRuntime
}

func (c *claraRuntimeBuiltins) String() string        { return "<clara builtins>" }
func (c *claraRuntimeBuiltins) Type() string          { return "clara" }
func (c *claraRuntimeBuiltins) Freeze()               {}
func (c *claraRuntimeBuiltins) Truth() starlark.Bool  { return true }
func (c *claraRuntimeBuiltins) Hash() (uint32, error) { return 0, errors.New("unhashable") }
func (c *claraRuntimeBuiltins) Attr(name string) (starlark.Value, error) {
	switch name {
	case "describe":
		return starlark.NewBuiltin("describe", c.rt.noopBuiltin), nil
	case "task":
		return starlark.NewBuiltin("task", c.rt.noopBuiltin), nil
	case "on":
		return starlark.NewBuiltin("on", c.rt.noopBuiltin), nil
	case "wait":
		return starlark.NewBuiltin("wait", c.rt.waitBuiltin), nil
	case "search":
		return starlark.NewBuiltin("search", c.rt.searchBuiltin), nil
	default:
		return nil, nil
	}
}
func (c *claraRuntimeBuiltins) AttrNames() []string {
	return []string{"describe", "on", "search", "task", "wait"}
}
