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
		"init": starlark.NewBuiltin("init", loader.initBuiltin),
		"tool": starlark.NewBuiltin("tool", loader.noopBuiltin),
		"wait": starlark.NewBuiltin("wait", loader.noopBuiltin),
	})
	if err != nil {
		return nil, errors.Wrap(err, "compile starlark intent")
	}

	if !loader.initialized {
		return nil, errors.New("starlark intent must call init(...) at top level")
	}

	mainValue, ok := globals["main"]
	if !ok {
		return nil, errors.New("starlark intent must define main()")
	}
	if _, ok := mainValue.(starlark.Callable); !ok {
		return nil, errors.New("starlark intent main must be callable")
	}

	if loader.intent.ID == "" {
		loader.intent.ID = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}

	if err := loader.intent.Validate(); err != nil {
		return nil, err
	}
	return loader.intent, nil
}

type starlarkIntentLoader struct {
	intent      *Intent
	initialized bool
}

func (l *starlarkIntentLoader) initBuiltin(
	_ *starlark.Thread,
	_ *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	if l.initialized {
		return nil, errors.New("init(...) may only be called once")
	}

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

	l.intent.ID = id
	l.intent.Description = description
	l.intent.Mode = mode
	l.intent.Interval = interval
	l.intent.Schedule = schedule
	l.intent.Trigger = trigger
	l.initialized = true
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
