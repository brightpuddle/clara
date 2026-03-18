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
		"task": starlark.NewBuiltin("task", loader.taskBuiltin),
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
		if len(loader.intent.Tasks) == 0 {
			return nil, errors.New("starlark intent must define main() or tasks")
		}
		for _, task := range loader.intent.Tasks {
			if task.Mode == IntentModeOnDemand {
				return nil, errors.New("starlark intent must define main() when tasks include on_demand handlers")
			}
		}
	} else if _, ok := mainValue.(starlark.Callable); !ok {
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
		tasks       *starlark.List
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
		"tasks?",
		&tasks,
	); err != nil {
		return nil, errors.Wrap(err, "parse init arguments")
	}

	l.intent.ID = id
	l.intent.Description = description
	l.intent.Mode = mode
	l.intent.Interval = interval
	l.intent.Schedule = schedule
	l.intent.Trigger = trigger

	if tasks != nil {
		for i := 0; i < tasks.Len(); i++ {
			st, ok := tasks.Index(i).(*starlarkTask)
			if !ok {
				return nil, errors.Newf("tasks[%d] must be a task(...) value", i)
			}
			l.intent.Tasks = append(l.intent.Tasks, st.task)
		}
	}

	l.initialized = true
	return starlark.None, nil
}

func (l *starlarkIntentLoader) taskBuiltin(
	_ *starlark.Thread,
	_ *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var (
		handler  starlark.Callable
		mode     string
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
		"mode?",
		&mode,
		"interval?",
		&interval,
		"schedule?",
		&schedule,
		"trigger?",
		&trigger,
	); err != nil {
		return nil, errors.Wrap(err, "parse task arguments")
	}

	if mode == "" {
		mode = IntentModeOnDemand
		if interval != "" {
			mode = IntentModeWorker
		} else if schedule != "" {
			mode = IntentModeSchedule
		} else if trigger != "" {
			mode = IntentModeEvent
		}
	}

	return &starlarkTask{
		task: Task{
			Handler:  handler.Name(),
			Mode:     mode,
			Interval: interval,
			Schedule: schedule,
			Trigger:  trigger,
		},
	}, nil
}

type starlarkTask struct {
	task Task
}

func (t *starlarkTask) String() string        { return "task(handler=" + t.task.Handler + ")" }
func (t *starlarkTask) Type() string          { return "task" }
func (t *starlarkTask) Freeze()               {}
func (t *starlarkTask) Truth() starlark.Bool  { return true }
func (t *starlarkTask) Hash() (uint32, error) { return 0, errors.New("unhashable type: task") }

func (l *starlarkIntentLoader) noopBuiltin(
	_ *starlark.Thread,
	_ *starlark.Builtin,
	_ starlark.Tuple,
	_ []starlark.Tuple,
) (starlark.Value, error) {
	return starlark.None, nil
}
