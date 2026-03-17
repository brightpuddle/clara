package interpreter

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"
	"go.starlark.net/starlark"
)

const starlarkScriptState = "SCRIPT"

type ReplayEntry struct {
	Sequence int
	Kind     string
	Name     string
	Args     any
	Result   any
	Error    string
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

	runtime := &starlarkRuntime{
		ctx:           ctx,
		runID:         opts.RunID,
		intentID:      intent.ID,
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
			runtime.log.Debug().Msg(msg)
		},
	}

	predeclared := starlark.StringDict{
		"init": starlark.NewBuiltin("init", runtime.initBuiltin),
		"tool": starlark.NewBuiltin("tool", runtime.toolBuiltin),
		"wait": starlark.NewBuiltin("wait", runtime.waitBuiltin),
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
	if !ok {
		return errors.New("starlark workflow must define main()")
	}
	mainFn, ok := mainValue.(starlark.Callable)
	if !ok {
		return errors.New("starlark workflow main must be callable")
	}
	mainResult, err := starlark.Call(thread, mainFn, nil, nil)
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
		if result, err := starlarkValueToGo(mainResult); err == nil {
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
	reg           *registry.Registry
	wait          WaitFunc
	onStep        StepFunc
	appendHistory HistoryAppendFunc
	history       []ReplayEntry
	cursor        int
	log           zerolog.Logger
}

func (rt *starlarkRuntime) initBuiltin(
	_ *starlark.Thread,
	_ *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var (
		id          string
		description string
		mode        string
		interval    string
		schedule    string
		trigger     string
	)
	if err := starlark.UnpackArgs(
		"init",
		args,
		kwargs,
		"id",
		&id,
		"description?",
		&description,
		"mode?",
		&mode,
		"interval?",
		&interval,
		"schedule?",
		&schedule,
		"trigger?",
		&trigger,
	); err != nil {
		return nil, errors.Wrap(err, "parse init arguments")
	}
	return starlark.None, nil
}

func (rt *starlarkRuntime) toolBuiltin(
	_ *starlark.Thread,
	_ *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	name, callArgs, err := parseBuiltinCall(args, kwargs)
	if err != nil {
		return nil, err
	}
	result, err := rt.invoke("tool", name, callArgs)
	if err != nil {
		return nil, err
	}
	return goToStarlark(result)
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
	return goToStarlark(result)
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
	if entry.Kind != kind || entry.Name != name || !reflect.DeepEqual(entry.Args, args) {
		return ReplayEntry{}, false, errors.Newf(
			"starlark replay divergence at sequence %d: expected %s %q with args %#v",
			entry.Sequence,
			entry.Kind,
			entry.Name,
			entry.Args,
		)
	}
	rt.cursor++
	return entry, true, nil
}

func (rt *starlarkRuntime) recordFresh(
	kind, name string,
	args map[string]any,
	result any,
	callErr error,
) error {
	entry := ReplayEntry{
		Sequence: rt.cursor,
		Kind:     kind,
		Name:     name,
		Args:     cloneMap(args),
		Result:   result,
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
		RunID:    rt.runID,
		IntentID: rt.intentID,
		State:    starlarkScriptState,
		Action:   action,
		Args:     args,
		Result:   result,
		Error:    errText,
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
		converted, err := starlarkValueToGo(dict)
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
		value, err := starlarkValueToGo(kw.Index(1))
		if err != nil {
			return "", nil, err
		}
		callArgs[key] = value
	}
	return name, callArgs, nil
}

func starlarkValueToGo(value starlark.Value) (any, error) {
	switch v := value.(type) {
	case starlark.NoneType:
		return nil, nil
	case starlark.Bool:
		return bool(v), nil
	case starlark.String:
		return v.GoString(), nil
	case starlark.Int:
		if i, ok := v.Int64(); ok {
			return i, nil
		}
		return nil, errors.Newf("integer %s exceeds int64", v.String())
	case starlark.Float:
		return float64(v), nil
	case *starlark.List:
		items := make([]any, 0, v.Len())
		for i := 0; i < v.Len(); i++ {
			item, err := starlarkValueToGo(v.Index(i))
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return items, nil
	case starlark.Tuple:
		items := make([]any, 0, v.Len())
		for i := 0; i < v.Len(); i++ {
			item, err := starlarkValueToGo(v.Index(i))
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return items, nil
	case *starlark.Dict:
		items := make(map[string]any, v.Len())
		for _, item := range v.Items() {
			key, ok := starlark.AsString(item[0])
			if !ok {
				return nil, errors.New("starlark dict keys must be strings")
			}
			converted, err := starlarkValueToGo(item[1])
			if err != nil {
				return nil, err
			}
			items[key] = converted
		}
		return items, nil
	default:
		return nil, errors.Newf("unsupported starlark value %s", value.Type())
	}
}

func goToStarlark(value any) (starlark.Value, error) {
	switch v := value.(type) {
	case nil:
		return starlark.None, nil
	case bool:
		return starlark.Bool(v), nil
	case string:
		return starlark.String(v), nil
	case time.Time:
		return starlark.String(v.UTC().Format(time.RFC3339Nano)), nil
	case int:
		return starlark.MakeInt(v), nil
	case int64:
		return starlark.MakeInt64(v), nil
	case int32:
		return starlark.MakeInt64(int64(v)), nil
	case uint:
		return starlark.MakeUint64(uint64(v)), nil
	case uint64:
		return starlark.MakeUint64(v), nil
	case float32:
		return starlark.Float(v), nil
	case float64:
		return starlark.Float(v), nil
	case []any:
		values := make([]starlark.Value, 0, len(v))
		for _, item := range v {
			converted, err := goToStarlark(item)
			if err != nil {
				return nil, err
			}
			values = append(values, converted)
		}
		return starlark.NewList(values), nil
	case []map[string]any:
		values := make([]starlark.Value, 0, len(v))
		for _, item := range v {
			converted, err := goToStarlark(item)
			if err != nil {
				return nil, err
			}
			values = append(values, converted)
		}
		return starlark.NewList(values), nil
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		dict := starlark.NewDict(len(v))
		for _, key := range keys {
			converted, err := goToStarlark(v[key])
			if err != nil {
				return nil, err
			}
			if err := dict.SetKey(starlark.String(key), converted); err != nil {
				return nil, errors.Wrapf(err, "set dict key %q", key)
			}
		}
		return dict, nil
	default:
		rv := reflect.ValueOf(value)
		switch rv.Kind() {
		case reflect.Slice, reflect.Array:
			length := rv.Len()
			values := make([]starlark.Value, 0, length)
			for i := 0; i < length; i++ {
				converted, err := goToStarlark(rv.Index(i).Interface())
				if err != nil {
					return nil, err
				}
				values = append(values, converted)
			}
			return starlark.NewList(values), nil
		case reflect.Map:
			dict := starlark.NewDict(rv.Len())
			keys := make([]string, 0, rv.Len())
			iter := rv.MapRange()
			for iter.Next() {
				if iter.Key().Kind() != reflect.String {
					return nil, errors.New("go map keys must be strings")
				}
				keys = append(keys, iter.Key().String())
			}
			sort.Strings(keys)
			for _, key := range keys {
				converted, err := goToStarlark(rv.MapIndex(reflect.ValueOf(key)).Interface())
				if err != nil {
					return nil, err
				}
				if err := dict.SetKey(starlark.String(key), converted); err != nil {
					return nil, err
				}
			}
			return dict, nil
		case reflect.Int8, reflect.Int16:
			return starlark.MakeInt64(rv.Int()), nil
		case reflect.Uint8, reflect.Uint16, reflect.Uint32:
			return starlark.MakeUint64(rv.Uint()), nil
		case reflect.Float64, reflect.Float32:
			return starlark.Float(rv.Float()), nil
		}
		return nil, errors.Newf("unsupported go value %T", value)
	}
}

func globalsToGo(globals starlark.StringDict) map[string]any {
	result := make(map[string]any, len(globals))
	for key, value := range globals {
		converted, err := starlarkValueToGo(value)
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
