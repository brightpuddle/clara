package orchestrator

import (
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
	"go.starlark.net/lib/json"
	"go.starlark.net/starlark"
)

func LoadIntentFile(path string, data []byte, namespaces []string) (*Intent, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".star":
		return CompileStarlarkIntent(path, string(data), namespaces)
	default:
		return nil, errors.Newf("unsupported intent file %q: only .star files are supported", path)
	}
}

func CompileStarlarkIntent(path, script string, namespaces []string) (*Intent, error) {
	loader := &starlarkIntentLoader{
		intent: &Intent{
			WorkflowType: WorkflowTypeStarlark,
			Script:       script,
		},
	}

	predeclared := starlark.StringDict{
		"clara": &claraBuiltins{loader: loader},
		"tui":   &dummyNamespaceProxy{name: "tui", namespaces: namespaces},
		"json":  json.Module,
		"yaml":  YAMLModule,
		"must":  MustModule,
	}
	for _, ns := range namespaces {
		if ns == "clara" || ns == "tui" {
			continue
		}
		predeclared[ns] = &dummyNamespaceProxy{name: ns, namespaces: namespaces}
	}

	thread := &starlark.Thread{Name: filepath.Base(path)}
	globals, err := starlark.ExecFile(thread, path, script, predeclared)
	if err != nil {
		return nil, errors.Wrap(err, "compile starlark intent")
	}

	// Collect tests from globals (names starting with test_)
	for name, val := range globals {
		if strings.HasPrefix(name, "test_") {
			if _, ok := val.(starlark.Callable); ok {
				loader.intent.Tests = append(loader.intent.Tests, name)
			}
		}
	}

	// Auto-register main() as an on_demand task if no explicit task() calls were made.
	if len(loader.intent.Tasks) == 0 {
		mainValue, ok := globals["main"]
		if !ok {
			if len(loader.intent.Tests) > 0 {
				// Valid as a test file even without main()
				goto skipMain
			}
			return nil, errors.New(
				"starlark intent must define main() or register tasks with task(...)",
			)
		}
		if _, ok := mainValue.(starlark.Callable); !ok {
			return nil, errors.New("starlark intent main must be callable")
		}
		task := Task{
			Handler: "main",
			Mode:    IntentModeOnDemand,
		}
		if fn, ok := mainValue.(*starlark.Function); ok {
			task.Parameters = extractParameters(fn)
		}
		loader.intent.Tasks = []Task{task}
	}

skipMain:

	// Derive the intent ID from the filename if not set (describe() does not set it).
	loader.intent.ID = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))

	if err := loader.intent.Validate(); err != nil {
		return nil, err
	}
	return loader.intent, nil
}

func extractParameters(fn *starlark.Function) []Parameter {
	numParams := fn.NumParams()
	params := make([]Parameter, 0, numParams)
	for i := 0; i < numParams; i++ {
		name, _ := fn.Param(i)
		params = append(params, Parameter{
			Name:     name,
			Required: fn.ParamDefault(i) == nil,
		})
	}
	return params
}

type starlarkIntentLoader struct {
	intent    *Intent
	described bool
}

// describeBuiltin handles: describe("Human-readable description of this intent")
func (l *starlarkIntentLoader) describeBuiltin(
	_ *starlark.Thread,
	_ *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	if l.described {
		return nil, errors.New("describe(...) may only be called once")
	}
	var description string
	if err := starlark.UnpackPositionalArgs("describe", args, kwargs, 1, &description); err != nil {
		return nil, errors.Wrap(err, "parse describe arguments")
	}
	l.intent.Description = description
	l.described = true
	return starlark.None, nil
}

// taskBuiltin handles: task(handler, trigger=, schedule=, interval=)
func (l *starlarkIntentLoader) taskBuiltin(
	_ *starlark.Thread,
	_ *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var (
		handler     starlark.Callable
		interval    string
		schedule    string
		triggerVal  starlark.Value
		triggerName string
		triggerArgs map[string]any
	)
	if err := starlark.UnpackArgs(
		"task",
		args,
		kwargs,
		"handler",
		&handler,
		"interval?",
		&interval,
		"schedule?",
		&schedule,
		"trigger?",
		&triggerVal,
	); err != nil {
		return nil, errors.Wrap(err, "parse task arguments")
	}

	mode := IntentModeOnDemand
	switch {
	case triggerVal != nil:
		mode = IntentModeEvent
		switch v := triggerVal.(type) {
		case starlark.String:
			triggerName = v.GoString()
		case *triggerValue:
			triggerName = v.name
			triggerArgs = v.args
		default:
			return nil, errors.Newf("trigger must be a string or on(...), got %s", triggerVal.Type())
		}
	case schedule != "":
		mode = IntentModeSchedule
	case interval != "":
		mode = IntentModeWorker
	}

	task := Task{
		Handler:     handler.Name(),
		Mode:        mode,
		Interval:    interval,
		Schedule:    schedule,
		Trigger:     triggerName,
		TriggerArgs: triggerArgs,
	}
	if fn, ok := handler.(*starlark.Function); ok {
		task.Parameters = extractParameters(fn)
	}
	l.intent.Tasks = append(l.intent.Tasks, task)
	return starlark.None, nil
}

func (l *starlarkIntentLoader) onBuiltin(
	_ *starlark.Thread,
	_ *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	if len(args) == 0 {
		return nil, errors.New(
			"on() requires at least one positional argument (the trigger tool reference)",
		)
	}
	fn, ok := args[0].(*starlark.Builtin)
	if !ok {
		return nil, errors.Newf("trigger must be a tool reference, got %s", args[0].Type())
	}
	name := fn.Name()

	if !strings.Contains(name, ".") {
		return nil, errors.Newf(
			"invalid trigger %q: must be a namespaced tool reference (e.g. theme.on_change)",
			name,
		)
	}

	// Restore underscores that were sanitized by dummyNamespaceProxy.
	name = strings.ReplaceAll(name, "-", "_")

	triggerArgs := make(map[string]any)
	for _, kw := range kwargs {
		key, ok := starlark.AsString(kw.Index(0))
		if !ok {
			return nil, errors.New("keyword argument names must be strings")
		}
		val, err := StarlarkValueToGo(kw.Index(1))
		if err != nil {
			return nil, err
		}
		triggerArgs[key] = val
	}

	return &triggerValue{
		name: name,
		args: triggerArgs,
	}, nil
}

type triggerValue struct {
	name string
	args map[string]any
}

func (v *triggerValue) String() string        { return "trigger(" + v.name + ")" }
func (v *triggerValue) Type() string          { return "trigger" }
func (v *triggerValue) Freeze()               {}
func (v *triggerValue) Truth() starlark.Bool  { return starlark.True }
func (v *triggerValue) Hash() (uint32, error) { return 0, errors.New("unhashable") }

func (l *starlarkIntentLoader) noopBuiltin(
	_ *starlark.Thread,
	_ *starlark.Builtin,
	_ starlark.Tuple,
	_ []starlark.Tuple,
) (starlark.Value, error) {
	return starlark.None, nil
}

type claraBuiltins struct {
	loader *starlarkIntentLoader
}

func (c *claraBuiltins) String() string        { return "<clara builtins>" }
func (c *claraBuiltins) Type() string          { return "clara" }
func (c *claraBuiltins) Freeze()               {}
func (c *claraBuiltins) Truth() starlark.Bool  { return true }
func (c *claraBuiltins) Hash() (uint32, error) { return 0, errors.New("unhashable") }
func (c *claraBuiltins) Attr(name string) (starlark.Value, error) {
	switch name {
	case "describe":
		return starlark.NewBuiltin("describe", c.loader.describeBuiltin), nil
	case "task":
		return starlark.NewBuiltin("task", c.loader.taskBuiltin), nil
	case "on":
		return starlark.NewBuiltin("on", c.loader.onBuiltin), nil
	case "wait":
		return starlark.NewBuiltin("wait", c.loader.noopBuiltin), nil
	case "search":
		return starlark.NewBuiltin("search", c.loader.noopBuiltin), nil
	default:
		return nil, nil
	}
}
func (c *claraBuiltins) AttrNames() []string {
	return []string{"describe", "on", "search", "task", "wait"}
}

type dummyNamespaceProxy struct {
	name       string
	namespaces []string
}

func (p *dummyNamespaceProxy) String() string        { return "<namespace " + p.name + ">" }
func (p *dummyNamespaceProxy) Type() string          { return "namespace" }
func (p *dummyNamespaceProxy) Freeze()               {}
func (p *dummyNamespaceProxy) Truth() starlark.Bool  { return true }
func (p *dummyNamespaceProxy) Hash() (uint32, error) { return 0, errors.New("unhashable") }
func (p *dummyNamespaceProxy) Attr(name string) (starlark.Value, error) {
	sanitized := strings.ReplaceAll(name, "_", "-")
	fqName := p.name + "." + sanitized
	// If the name is known as a namespace, return another dummy proxy for further nesting.
	for _, ns := range p.namespaces {
		if strings.HasPrefix(ns, fqName+".") {
			return &dummyNamespaceProxy{name: fqName, namespaces: p.namespaces}, nil
		}
	}
	return starlark.NewBuiltin(
		fqName,
		func(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			return starlark.None, nil
		},
	), nil
}
func (p *dummyNamespaceProxy) AttrNames() []string { return nil }
