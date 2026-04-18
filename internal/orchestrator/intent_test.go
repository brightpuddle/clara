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

func TestCompileStarlarkIntent_ValidateTriggers(t *testing.T) {
	// Passing 'theme' as a known namespace.
	namespaces := []string{"theme", "fs"}

	// Valid trigger in known namespace.
	_, err := orchestrator.CompileStarlarkIntent("/tmp/valid.star", `
def on_event(event):
    return None
clara.task(on_event, trigger = clara.on(theme.on_change))
`, namespaces)
	if err != nil {
		t.Fatalf("expected valid intent, got error: %v", err)
	}

	// Invalid trigger: unknown namespace.
	_, err = orchestrator.CompileStarlarkIntent("/tmp/invalid.star", `
def on_event(event):
    return None
clara.task(on_event, trigger = clara.on(unknown.on_change))
`, namespaces)
	if err == nil {
		t.Fatal("expected error for unknown trigger namespace")
	}

	// Invalid trigger: missing namespace (e.g. clara.on(on_change))
	// Starlark will throw AttributeError or undefined if on_change is not defined.
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

func TestIntentValidate_TaskInvalidMode(t *testing.T) {
	intent := &orchestrator.Intent{
		ID:           "bad-mode",
		WorkflowType: orchestrator.WorkflowTypeStarlark,
		Script:       `def main(): return None`,
		Tasks: []orchestrator.Task{
			{Handler: "main", Mode: "forever"},
		},
	}
	if err := intent.Validate(); err == nil {
		t.Fatal("expected invalid task mode to be rejected")
	}
}

func TestIntentValidate_TaskWorkerRequiresValidInterval(t *testing.T) {
	cases := []orchestrator.Intent{
		{
			ID:           "worker-missing-interval",
			WorkflowType: orchestrator.WorkflowTypeStarlark,
			Script:       `def main(): return None`,
			Tasks: []orchestrator.Task{
				{Handler: "main", Mode: orchestrator.IntentModeWorker},
			},
		},
		{
			ID:           "worker-bad-interval",
			WorkflowType: orchestrator.WorkflowTypeStarlark,
			Script:       `def main(): return None`,
			Tasks: []orchestrator.Task{
				{Handler: "main", Mode: orchestrator.IntentModeWorker, Interval: "tomorrow"},
			},
		},
	}
	for _, intent := range cases {
		if err := intent.Validate(); err == nil {
			t.Fatalf("expected worker validation failure for %#v", intent)
		}
	}
}

// TestCompileStarlarkIntent verifies the new top-level task() API.
func TestCompileStarlarkIntent(t *testing.T) {
	intent, err := orchestrator.CompileStarlarkIntent("/tmp/weather.star", `
clara.describe("Notify when rain is forecast")

def main():
    return None

clara.task(main, schedule = "0 7 * * *")
`, nil)
	if err != nil {
		t.Fatalf("CompileStarlarkIntent failed: %v", err)
	}
	if intent.ID != "weather" {
		t.Fatalf("unexpected id: %q", intent.ID)
	}
	if intent.Description != "Notify when rain is forecast" {
		t.Fatalf("unexpected description: %q", intent.Description)
	}
	if len(intent.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(intent.Tasks))
	}
	task := intent.Tasks[0]
	if task.Mode != orchestrator.IntentModeSchedule {
		t.Fatalf("unexpected mode: %q", task.Mode)
	}
	if task.Schedule != "0 7 * * *" {
		t.Fatalf("unexpected schedule: %q", task.Schedule)
	}
	if intent.WorkflowKind() != orchestrator.WorkflowTypeStarlark {
		t.Fatalf("unexpected workflow kind: %q", intent.WorkflowKind())
	}
}

func TestCompileStarlarkIntent_WorkerInterval(t *testing.T) {
	intent, err := orchestrator.CompileStarlarkIntent("/tmp/indexer.star", `
def main():
    return None

clara.task(main, interval = "15m")
`, nil)
	if err != nil {
		t.Fatalf("CompileStarlarkIntent failed: %v", err)
	}
	if len(intent.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(intent.Tasks))
	}
	task := intent.Tasks[0]
	if task.Mode != orchestrator.IntentModeWorker {
		t.Fatalf("unexpected mode: %q", task.Mode)
	}
	if task.Interval != "15m" {
		t.Fatalf("unexpected interval: %q", task.Interval)
	}
}

// TestCompileStarlarkIntent_ImplicitMain verifies that a file with main() but no
// explicit task() call automatically registers main as an on_demand task.
func TestCompileStarlarkIntent_ImplicitMain(t *testing.T) {
	intent, err := orchestrator.CompileStarlarkIntent("/tmp/hello.star", `
def main():
    return None
`, nil)
	if err != nil {
		t.Fatalf("CompileStarlarkIntent failed: %v", err)
	}
	if len(intent.Tasks) != 1 {
		t.Fatalf("expected 1 implicit task, got %d", len(intent.Tasks))
	}
	if intent.Tasks[0].Handler != "main" {
		t.Fatalf("expected handler 'main', got %q", intent.Tasks[0].Handler)
	}
	if intent.Tasks[0].Mode != orchestrator.IntentModeOnDemand {
		t.Fatalf("expected on_demand mode, got %q", intent.Tasks[0].Mode)
	}
}

// TestCompileStarlarkIntent_RequiresMainOrTask verifies that a file with neither
// main() nor any task() calls is rejected.
func TestCompileStarlarkIntent_RequiresMainOrTask(t *testing.T) {
	_, err := orchestrator.CompileStarlarkIntent("/tmp/empty.star", `
x = 1
`, nil)
	if err == nil {
		t.Fatal("expected compile error for file with no main() and no task() calls")
	}
}

func TestCompileStarlarkIntent_Tasks(t *testing.T) {
	intent, err := orchestrator.CompileStarlarkIntent("/tmp/tasks.star", `
def on_event(event):
    return event

def on_timer():
    return None

clara.task(on_event, trigger = "macos.reminders_changed")
clara.task(on_timer, schedule = "0 7 * * *")
`, nil)
	if err != nil {
		t.Fatalf("CompileStarlarkIntent failed: %v", err)
	}
	if len(intent.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(intent.Tasks))
	}
	if intent.Tasks[0].Handler != "on_event" ||
		intent.Tasks[0].Trigger != "macos.reminders_changed" {
		t.Fatalf("unexpected first task: %+v", intent.Tasks[0])
	}
	if intent.Tasks[1].Handler != "on_timer" || intent.Tasks[1].Schedule != "0 7 * * *" {
		t.Fatalf("unexpected second task: %+v", intent.Tasks[1])
	}
}

// TestCompileStarlarkIntent_IDFromFilename verifies that the intent ID is always
// derived from the file basename, ignoring any describe() call.
func TestCompileStarlarkIntent_IDFromFilename(t *testing.T) {
	intent, err := orchestrator.CompileStarlarkIntent("/tmp/my-intent.star", `
clara.describe("An intent whose ID comes from its filename")

def main():
    return None
`, nil)
	if err != nil {
		t.Fatalf("CompileStarlarkIntent failed: %v", err)
	}
	if intent.ID != "my-intent" {
		t.Fatalf("expected id 'my-intent', got %q", intent.ID)
	}
}

func TestLoadIntentFile_OnlySupportsStar(t *testing.T) {
	_, err := orchestrator.LoadIntentFile(
		filepath.Join("/tmp", "intent.yaml"),
		[]byte("id: unsupported"),
		nil,
	)
	if err == nil {
		t.Fatal("expected non-.star file to be rejected")
	}
}

func TestCompileStarlarkIntent_DescribeOnce(t *testing.T) {
	_, err := orchestrator.CompileStarlarkIntent("/tmp/dup.star", `
	clara.describe("first")
	clara.describe("second")

	def main():
	return None
	`, nil)
	if err == nil {
		t.Fatal("expected error for calling describe() twice")
	}
}

func TestCompileStarlarkIntent_ParameterExtraction(t *testing.T) {
	intent, err := orchestrator.CompileStarlarkIntent("/tmp/params.star", `
def main(name, count=1):
    pass
`, nil)
	if err != nil {
		t.Fatalf("CompileStarlarkIntent failed: %v", err)
	}

	if len(intent.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(intent.Tasks))
	}

	task := intent.Tasks[0]
	if len(task.Parameters) != 2 {
		t.Fatalf("expected 2 parameters, got %d", len(task.Parameters))
	}

	if task.Parameters[0].Name != "name" || !task.Parameters[0].Required {
		t.Errorf("unexpected parameter 0: %+v", task.Parameters[0])
	}

	if task.Parameters[1].Name != "count" || task.Parameters[1].Required {
		t.Errorf("unexpected parameter 1: %+v", task.Parameters[1])
	}
}

func TestParseIntent_Parameters(t *testing.T) {
	data := []byte(`
{
	"id": "params-test",
	"workflow_type": "starlark",
	"script": "def main(arg1): pass",
	"tasks": [
		{
			"handler": "main",
			"mode": "on_demand",
			"parameters": [
				{"name": "arg1", "required": true},
				{"name": "arg2", "required": false}
			]
		}
	]
}
`)
	intent, err := orchestrator.ParseIntent(data)
	if err != nil {
		t.Fatalf("ParseIntent failed: %v", err)
	}

	if len(intent.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(intent.Tasks))
	}

	task := intent.Tasks[0]
	if len(task.Parameters) != 2 {
		t.Fatalf("expected 2 parameters, got %d", len(task.Parameters))
	}

	if task.Parameters[0].Name != "arg1" || !task.Parameters[0].Required {
		t.Errorf("unexpected parameter 0: %+v", task.Parameters[0])
	}

	if task.Parameters[1].Name != "arg2" || task.Parameters[1].Required {
		t.Errorf("unexpected parameter 1: %+v", task.Parameters[1])
	}
}

func TestCompileStarlarkIntent_Tests(t *testing.T) {
	intent, err := orchestrator.CompileStarlarkIntent("/tmp/tests.star", `
def main():
    pass

def test_logic():
    must.eq(1, 1)

def test_other():
    must.true(True)

not_a_test = lambda: None
`, nil)
	if err != nil {
		t.Fatalf("CompileStarlarkIntent failed: %v", err)
	}

	expectedTests := map[string]bool{
		"test_logic": true,
		"test_other": true,
	}

	if len(intent.Tests) != len(expectedTests) {
		t.Fatalf("expected %d tests, got %d: %v", len(expectedTests), len(intent.Tests), intent.Tests)
	}

	for _, name := range intent.Tests {
		if !expectedTests[name] {
			t.Errorf("unexpected test extracted: %q", name)
		}
	}
}
