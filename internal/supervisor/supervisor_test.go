package supervisor_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/brightpuddle/clara/internal/interpreter"
	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/supervisor"
	"github.com/rs/zerolog"
)

func newTestSupervisor(t *testing.T, tasksDir string) (*supervisor.Supervisor, *registry.Registry) {
	t.Helper()
	log := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).With().Timestamp().Logger()
	reg := registry.New(log)
	starlarkIt := interpreter.NewStarlark(reg, log)
	sup := supervisor.New(tasksDir, reg, func(
		ctx context.Context,
		intent *orchestrator.Intent,
		runID string,
		entrypoint string,
		args any,
	) error {
		return starlarkIt.Execute(ctx, intent, "", interpreter.RunOptions{
			RunID:       runID,
			Entrypoint:  entrypoint,
			HandlerArgs: args,
		})
	}, log)
	return sup, reg
}

func TestSupervisor_EventTrigger(t *testing.T) {
	dir := t.TempDir()
	sup, reg := newTestSupervisor(t, dir)

	script := `
def on_event(event):
    # Use a tool to signify we were called
    tool("test.verify", data=event["foo"])

init(
    id = "event-test",
    tasks = [
        task(trigger = "mock.test_event", handler = on_event)
    ]
)
`
	if err := os.WriteFile(filepath.Join(dir, "event.star"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}

	verified := make(chan string, 1)
	reg.Register("test.verify", func(_ context.Context, args map[string]any) (any, error) {
		verified <- args["data"].(string)
		return nil, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sup.Start(ctx) //nolint:errcheck

	// Wait for loader to pick up the file
	time.Sleep(200 * time.Millisecond)

	// Emit notification
	reg.EmitNotification("mock", "test_event", map[string]any{"foo": "bar"})

	select {
	case data := <-verified:
		if data != "bar" {
			t.Errorf("expected bar, got %s", data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for handler to be called")
	}
}

func TestSupervisor_LegacyEventTrigger(t *testing.T) {
	dir := t.TempDir()
	sup, reg := newTestSupervisor(t, dir)

	script := `
def main(event):
    tool("test.verify", data=event["foo"])

init(
    id = "legacy-test",
    mode = "event",
    trigger = "mock.legacy_event"
)
`
	if err := os.WriteFile(filepath.Join(dir, "legacy.star"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}

	verified := make(chan string, 1)
	reg.Register("test.verify", func(_ context.Context, args map[string]any) (any, error) {
		verified <- args["data"].(string)
		return nil, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sup.Start(ctx) //nolint:errcheck

	time.Sleep(200 * time.Millisecond)

	reg.EmitNotification("mock", "legacy_event", map[string]any{"foo": "legacy"})

	select {
	case data := <-verified:
		if data != "legacy" {
			t.Errorf("expected legacy, got %s", data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for legacy handler to be called")
	}
}

func TestSupervisor_LegacyEventWithoutTriggerRunsOnce(t *testing.T) {
	dir := t.TempDir()
	sup, reg := newTestSupervisor(t, dir)

	script := `
def main():
    tool("test.verify", value = "started")

init(
    id = "legacy-event-no-trigger",
    mode = "event",
)
`
	if err := os.WriteFile(filepath.Join(dir, "legacy-no-trigger.star"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}

	called := make(chan string, 1)
	reg.Register("test.verify", func(_ context.Context, args map[string]any) (any, error) {
		called <- args["value"].(string)
		return nil, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sup.Start(ctx) //nolint:errcheck

	select {
	case got := <-called:
		if got != "started" {
			t.Fatalf("expected started, got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for legacy event intent to run")
	}
}

func TestSupervisor_MultiTaskIntentStaysActiveWhileOneTaskStops(t *testing.T) {
	dir := t.TempDir()
	sup, reg := newTestSupervisor(t, dir)

	script := `
def on_event(event):
    tool("test.verify", value = event["foo"])

def broken_schedule():
    return None

init(
    id = "multi-task-active",
    tasks = [
        task(handler = broken_schedule, mode = "schedule", schedule = "not-a-cron"),
        task(handler = on_event, trigger = "mock.active"),
    ],
)
`
	if err := os.WriteFile(filepath.Join(dir, "multi-task.star"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}

	verified := make(chan string, 1)
	reg.Register("test.verify", func(_ context.Context, args map[string]any) (any, error) {
		verified <- args["value"].(string)
		return nil, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sup.Start(ctx) //nolint:errcheck
	time.Sleep(300 * time.Millisecond)

	infos := sup.IntentInfos()
	if len(infos) != 1 || !infos[0].Active {
		t.Fatalf("expected multi-task intent to remain active, got %+v", infos)
	}

	reg.EmitNotification("mock", "active", map[string]any{"foo": "bar"})
	select {
	case got := <-verified:
		if got != "bar" {
			t.Fatalf("expected bar, got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event task to run")
	}
}

func validIntentStar(id string) []byte {
	return []byte("init(id = \"" + id + "\")\n\ndef main():\n    return None\n")
}

func TestSupervisor_ValidateIntent_Valid(t *testing.T) {
	sup, reg := newTestSupervisor(t, t.TempDir())

	reg.Register("my.tool", func(_ context.Context, _ map[string]any) (any, error) {
		return nil, nil
	})

	intent := &orchestrator.Intent{
		ID:           "valid",
		InitialState: "RUN",
		States: map[string]orchestrator.State{
			"RUN": {Action: "my.tool", Terminal: true},
		},
	}
	if err := sup.ValidateIntent(intent); err != nil {
		t.Fatalf("expected valid intent, got: %v", err)
	}
}

func TestSupervisor_ValidateIntent_UnregisteredTool(t *testing.T) {
	sup, _ := newTestSupervisor(t, t.TempDir())

	intent := &orchestrator.Intent{
		ID:           "unregistered",
		InitialState: "RUN",
		States: map[string]orchestrator.State{
			"RUN": {Action: "nonexistent.tool", Terminal: true},
		},
	}
	err := sup.ValidateIntent(intent)
	if err == nil {
		t.Fatal("expected error for unregistered tool")
	}
}

func TestSupervisor_ValidateIntent_InvalidStructure(t *testing.T) {
	sup, _ := newTestSupervisor(t, t.TempDir())

	intent := &orchestrator.Intent{
		ID:     "", // missing required field
		States: map[string]orchestrator.State{},
	}
	if err := sup.ValidateIntent(intent); err == nil {
		t.Fatal("expected error for invalid intent structure")
	}
}

func TestSupervisor_LoadsExistingFiles(t *testing.T) {
	dir := t.TempDir()
	sup, _ := newTestSupervisor(t, dir)

	if err := os.WriteFile(filepath.Join(dir, "test.star"), validIntentStar("test-intent"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Start runs until ctx is cancelled; it should load existing files first.
	sup.Start(ctx) //nolint:errcheck

	intents := sup.ActiveIntents()
	if len(intents) == 0 {
		t.Error("expected at least one intent to be loaded from existing files")
	}
}
