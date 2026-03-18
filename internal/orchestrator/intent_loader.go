package orchestrator

import (
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
	"go.starlark.net/starlark"
)

func LoadIntentFile(path string, data []byte) (*Intent, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".star":
		return CompileStarlarkIntent(path, string(data))
	default:
		return nil, errors.Newf("unsupported intent file %q: only .star files are supported", path)
	}
}

func CompileStarlarkIntent(path, script string) (*Intent, error) {
	loader := &starlarkIntentLoader{
		intent: &Intent{
			WorkflowType: WorkflowTypeStarlark,
			Script:       script,
		},
	}

	thread := &starlark.Thread{Name: filepath.Base(path)}
	globals, err := starlark.ExecFile(thread, path, script, starlark.StringDict{
		"describe": starlark.NewBuiltin("describe", loader.describeBuiltin),
		"task":     starlark.NewBuiltin("task", loader.taskBuiltin),
		"tool":     starlark.NewBuiltin("tool", loader.noopBuiltin),
		"wait":     starlark.NewBuiltin("wait", loader.noopBuiltin),
	})
	if err != nil {
		return nil, errors.Wrap(err, "compile starlark intent")
	}

	// Auto-register main() as an on_demand task if no explicit task() calls were made.
	if len(loader.intent.Tasks) == 0 {
		mainValue, ok := globals["main"]
		if !ok {
			return nil, errors.New(
				"starlark intent must define main() or register tasks with task(...)",
			)
		}
		if _, ok := mainValue.(starlark.Callable); !ok {
			return nil, errors.New("starlark intent main must be callable")
		}
		loader.intent.Tasks = []Task{{
			Handler: "main",
			Mode:    IntentModeOnDemand,
		}}
	}

	// Derive the intent ID from the filename if not set (describe() does not set it).
	loader.intent.ID = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))

	if err := loader.intent.Validate(); err != nil {
		return nil, err
	}
	return loader.intent, nil
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
// or: task(handler = fn, trigger = "...", ...)
// Mode is always inferred from the presence of trigger/schedule/interval.
func (l *starlarkIntentLoader) taskBuiltin(
	_ *starlark.Thread,
	_ *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var (
		handler  starlark.Callable
		interval string
		schedule string
		trigger  string
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
		&trigger,
	); err != nil {
		return nil, errors.Wrap(err, "parse task arguments")
	}

	mode := IntentModeOnDemand
	switch {
	case trigger != "":
		mode = IntentModeEvent
	case schedule != "":
		mode = IntentModeSchedule
	case interval != "":
		mode = IntentModeWorker
	}

	l.intent.Tasks = append(l.intent.Tasks, Task{
		Handler:  handler.Name(),
		Mode:     mode,
		Interval: interval,
		Schedule: schedule,
		Trigger:  trigger,
	})
	return starlark.None, nil
}

func (l *starlarkIntentLoader) noopBuiltin(
	_ *starlark.Thread,
	_ *starlark.Builtin,
	_ starlark.Tuple,
	_ []starlark.Tuple,
) (starlark.Value, error) {
	return starlark.None, nil
}
