package orchestrator_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/brightpuddle/clara/internal/orchestrator"
)

func TestIntentValidate_Valid(t *testing.T) {
	b := &orchestrator.Intent{
		ID:           "test-intent",
		InitialState: "START",
		States: map[string]orchestrator.State{
			"START": {
				Next: "END",
			},
			"END": {
				Terminal: true,
			},
		},
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("expected valid intent, got error: %v", err)
	}
}

func TestIntentValidate_MissingID(t *testing.T) {
	b := &orchestrator.Intent{
		InitialState: "START",
		States: map[string]orchestrator.State{
			"START": {Terminal: true},
		},
	}
	err := b.Validate()
	if err == nil {
		t.Fatal("expected error for missing id")
	}
}

func TestIntentValidate_MissingInitialState(t *testing.T) {
	b := &orchestrator.Intent{
		ID: "test",
		States: map[string]orchestrator.State{
			"START": {Terminal: true},
		},
	}
	err := b.Validate()
	if err == nil {
		t.Fatal("expected error for missing initial_state")
	}
}

func TestIntentValidate_InitialStateNotFound(t *testing.T) {
	b := &orchestrator.Intent{
		ID:           "test",
		InitialState: "MISSING",
		States: map[string]orchestrator.State{
			"START": {Terminal: true},
		},
	}
	err := b.Validate()
	if err == nil {
		t.Fatal("expected error for initial_state referencing nonexistent state")
	}
}

func TestIntentValidate_TransitionToMissingState(t *testing.T) {
	b := &orchestrator.Intent{
		ID:           "test",
		InitialState: "START",
		States: map[string]orchestrator.State{
			"START": {
				Transitions: []orchestrator.Transition{
					{Condition: "true", Next: "DOES_NOT_EXIST"},
				},
			},
		},
	}
	err := b.Validate()
	if err == nil {
		t.Fatal("expected error for transition referencing nonexistent state")
	}
}

func TestIntentValidate_NextToMissingState(t *testing.T) {
	b := &orchestrator.Intent{
		ID:           "test",
		InitialState: "START",
		States: map[string]orchestrator.State{
			"START": {
				Next: "DOES_NOT_EXIST",
			},
		},
	}
	err := b.Validate()
	if err == nil {
		t.Fatal("expected error for next referencing nonexistent state")
	}
}

func TestParseIntent_RoundTrip(t *testing.T) {
	input := &orchestrator.Intent{
		ID:           "roundtrip",
		Description:  "test",
		InitialState: "A",
		States: map[string]orchestrator.State{
			"A": {
				Action:  "some.tool",
				Args:    map[string]any{"key": "val"},
				ForEach: "LOAD",
				Item:    "row",
				Next:    "B",
			},
			"B": {Terminal: true},
		},
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	got, err := orchestrator.ParseIntent(data)
	if err != nil {
		t.Fatalf("ParseIntent failed: %v", err)
	}
	if got.ID != input.ID {
		t.Errorf("ID mismatch: got %q want %q", got.ID, input.ID)
	}
	if got.InitialState != input.InitialState {
		t.Errorf("InitialState mismatch: got %q want %q", got.InitialState, input.InitialState)
	}
	if got.States["A"].ForEach != "LOAD" {
		t.Errorf("ForEach mismatch: got %q want %q", got.States["A"].ForEach, "LOAD")
	}
	if got.States["A"].Item != "row" {
		t.Errorf("Item mismatch: got %q want %q", got.States["A"].Item, "row")
	}
}

func TestParseIntent_InvalidJSON(t *testing.T) {
	_, err := orchestrator.ParseIntent([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestIntentValidate_ValidStarlark(t *testing.T) {
	intent := &orchestrator.Intent{
		ID:           "starlark-intent",
		WorkflowType: orchestrator.WorkflowTypeStarlark,
		Script: `
init(id = "starlark-intent")

def main():
    return "ok"
`,
	}
	if err := intent.Validate(); err != nil {
		t.Fatalf("expected valid starlark intent, got %v", err)
	}
}

func TestIntentValidate_StarlarkRejectsStates(t *testing.T) {
	intent := &orchestrator.Intent{
		ID:           "mixed-intent",
		WorkflowType: orchestrator.WorkflowTypeStarlark,
		Script:       `pass`,
		InitialState: "START",
		States: map[string]orchestrator.State{
			"START": {Terminal: true},
		},
	}
	if err := intent.Validate(); err == nil {
		t.Fatal("expected mixed starlark/state intent to be rejected")
	}
}

func TestParseIntent_YAML(t *testing.T) {
	data := []byte(`
id: yaml-intent
description: yaml test
initial_state: START
states:
  START:
    action: some.tool
    args:
      key: value
    next: END
  END:
    terminal: true
`)

	intent, err := orchestrator.ParseIntent(data)
	if err != nil {
		t.Fatalf("ParseIntent YAML failed: %v", err)
	}
	if intent.ID != "yaml-intent" {
		t.Fatalf("unexpected intent ID: %q", intent.ID)
	}
	if intent.States["START"].Args["key"] != "value" {
		t.Fatalf("unexpected args: %#v", intent.States["START"].Args)
	}
}

func TestIntentValidate_InvalidMode(t *testing.T) {
	intent := &orchestrator.Intent{
		ID:           "bad-mode",
		Mode:         "forever",
		InitialState: "START",
		States: map[string]orchestrator.State{
			"START": {Terminal: true},
		},
	}
	if err := intent.Validate(); err == nil {
		t.Fatal("expected invalid mode to be rejected")
	}
}

func TestIntentValidate_WorkerRequiresValidInterval(t *testing.T) {
	cases := []orchestrator.Intent{
		{
			ID:           "worker-missing-interval",
			Mode:         orchestrator.IntentModeWorker,
			WorkflowType: orchestrator.WorkflowTypeStarlark,
			Script:       `def main(): return None`,
		},
		{
			ID:           "worker-bad-interval",
			Mode:         orchestrator.IntentModeWorker,
			Interval:     "tomorrow",
			WorkflowType: orchestrator.WorkflowTypeStarlark,
			Script:       `def main(): return None`,
		},
	}
	for _, intent := range cases {
		if err := intent.Validate(); err == nil {
			t.Fatalf("expected worker validation failure for %#v", intent)
		}
	}
}

func TestCompileStarlarkIntent(t *testing.T) {
	intent, err := orchestrator.CompileStarlarkIntent("/tmp/weather.star", `
init(
    id = "daily-weather",
    description = "Notify when rain is forecast",
    mode = "schedule",
    schedule = "0 7 * * *",
)

def main():
    return None
`)
	if err != nil {
		t.Fatalf("CompileStarlarkIntent failed: %v", err)
	}
	if intent.ID != "daily-weather" {
		t.Fatalf("unexpected id: %q", intent.ID)
	}
	if intent.Description != "Notify when rain is forecast" {
		t.Fatalf("unexpected description: %q", intent.Description)
	}
	if intent.Mode != orchestrator.IntentModeSchedule {
		t.Fatalf("unexpected mode: %q", intent.Mode)
	}
	if intent.Schedule != "0 7 * * *" {
		t.Fatalf("unexpected schedule: %q", intent.Schedule)
	}
	if intent.WorkflowKind() != orchestrator.WorkflowTypeStarlark {
		t.Fatalf("unexpected workflow kind: %q", intent.WorkflowKind())
	}
}

func TestCompileStarlarkIntent_WorkerInterval(t *testing.T) {
	intent, err := orchestrator.CompileStarlarkIntent("/tmp/indexer.star", `
init(
    id = "note-indexer",
    mode = "worker",
    interval = "15m",
)

def main():
    return None
`)
	if err != nil {
		t.Fatalf("CompileStarlarkIntent failed: %v", err)
	}
	if intent.Mode != orchestrator.IntentModeWorker {
		t.Fatalf("unexpected mode: %q", intent.Mode)
	}
	if intent.Interval != "15m" {
		t.Fatalf("unexpected interval: %q", intent.Interval)
	}
}

func TestCompileStarlarkIntent_RequiresInitAndMain(t *testing.T) {
	for name, script := range map[string]string{
		"missing init": "def main():\n    return None\n",
		"missing main": "init(id = \"missing-main\")\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := orchestrator.CompileStarlarkIntent("/tmp/test.star", script); err == nil {
				t.Fatal("expected compile error")
			}
		})
	}
}

func TestCompileStarlarkIntent_Tasks(t *testing.T) {
	intent, err := orchestrator.CompileStarlarkIntent("/tmp/tasks.star", `
def on_event(event):
    return event

def on_timer():
    return None

init(
    id = "tasks-test",
    tasks = [
        task(handler = on_event, trigger = "bridge.reminders_changed"),
        task(handler = on_timer, schedule = "0 7 * * *"),
    ],
)
`)
	if err != nil {
		t.Fatalf("CompileStarlarkIntent failed: %v", err)
	}
	if len(intent.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(intent.Tasks))
	}
	if intent.Tasks[0].Handler != "on_event" || intent.Tasks[0].Trigger != "bridge.reminders_changed" {
		t.Fatalf("unexpected first task: %+v", intent.Tasks[0])
	}
	if intent.Tasks[1].Handler != "on_timer" || intent.Tasks[1].Schedule != "0 7 * * *" {
		t.Fatalf("unexpected second task: %+v", intent.Tasks[1])
	}
}

func TestCompileStarlarkIntent_RejectsOnDemandTasksWithoutMain(t *testing.T) {
	_, err := orchestrator.CompileStarlarkIntent("/tmp/tasks.star", `
def background():
    return None

init(
    id = "bad-tasks",
    tasks = [task(handler = background)],
)
`)
	if err == nil {
		t.Fatal("expected on_demand task without main to be rejected")
	}
}

func TestLoadIntentFile_OnlySupportsStar(t *testing.T) {
	_, err := orchestrator.LoadIntentFile(
		filepath.Join("/tmp", "intent.yaml"),
		[]byte("id: unsupported"),
	)
	if err == nil {
		t.Fatal("expected non-.star file to be rejected")
	}
}
