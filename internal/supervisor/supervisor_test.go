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
	log := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).
		With().
		Timestamp().
		Logger()
	reg := registry.New(log)
	starlarkIt := interpreter.NewStarlark(reg, log)
	sup := supervisor.New(tasksDir, reg, 2*time.Second, func(
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
    test.verify(data=event["foo"])

clara.task(on_event, trigger = "mock.test_event")
`
	if err := os.WriteFile(filepath.Join(dir, "event.star"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}

	verified := make(chan string, 1)
	reg.Register("test.verify", func(_ context.Context, args map[string]any) (any, error) {
		verified <- args["data"].(string)
		return nil, nil
	})
	reg.Register("fs.wait_for_change", func(_ context.Context, args map[string]any) (any, error) {
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

func TestSupervisor_EventTriggerWithArgs(t *testing.T) {
	dir := t.TempDir()
	sup, reg := newTestSupervisor(t, dir)

	verified := make(chan string, 1)
	reg.Register("test.verify", func(_ context.Context, args map[string]any) (any, error) {
		verified <- args["data"].(string)
		return nil, nil
	})
	reg.Register("fs.wait_for_change", func(_ context.Context, args map[string]any) (any, error) {
		return nil, nil
	})

	script := `
def on_change(event):
    test.verify(data=event["path"])

clara.task(on_change, trigger = clara.on(fs.wait_for_change, path="/tmp/foo.txt"))
`
	if err := os.WriteFile(filepath.Join(dir, "trigger_args.star"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sup.Start(ctx) //nolint:errcheck

	// Wait for loader to pick up the file
	time.Sleep(200 * time.Millisecond)

	// Emit notification with wrong path - should not trigger
	reg.EmitNotification("fs", "wait_for_change", map[string]any{"path": "/tmp/wrong.txt"})

	select {
	case <-verified:
		t.Fatal("handler called for wrong path")
	case <-time.After(500 * time.Millisecond):
		// Good, not triggered
	}

	// Emit notification with correct path - should trigger
	reg.EmitNotification("fs", "wait_for_change", map[string]any{"path": "/tmp/foo.txt"})

	select {
	case data := <-verified:
		if data != "/tmp/foo.txt" {
			t.Errorf("expected /tmp/foo.txt, got %s", data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for handler to be called")
	}
}

func TestSupervisor_EventTriggerInline(t *testing.T) {
	dir := t.TempDir()
	sup, reg := newTestSupervisor(t, dir)

	verified := make(chan string, 1)
	reg.Register("test.verify", func(_ context.Context, args map[string]any) (any, error) {
		verified <- args["data"].(string)
		return nil, nil
	})
	reg.Register("fs.wait_for_change", func(_ context.Context, args map[string]any) (any, error) {
		return nil, nil
	})

	script := `
def main(event):
    test.verify(data=event["foo"])

clara.task(main, trigger = "mock.legacy_event")
`
	if err := os.WriteFile(filepath.Join(dir, "legacy.star"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}

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

func TestSupervisor_WorkerRunsOnInterval(t *testing.T) {
	dir := t.TempDir()
	sup, reg := newTestSupervisor(t, dir)

	called := make(chan string, 1)
	reg.Register("test.verify", func(_ context.Context, args map[string]any) (any, error) {
		select {
		case called <- args["value"].(string):
		default:
		}
		return nil, nil
	})

	script := `
def run():
    test.verify(value = "started")

clara.task(run, interval = "100ms")
`
	if err := os.WriteFile(filepath.Join(dir, "legacy-no-trigger.star"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sup.Start(ctx) //nolint:errcheck

	select {
	case got := <-called:
		if got != "started" {
			t.Fatalf("expected started, got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for worker task to run")
	}
}

func TestSupervisor_MultiTaskIntentStaysActiveWhileOneTaskStops(t *testing.T) {
	dir := t.TempDir()
	sup, reg := newTestSupervisor(t, dir)

	verified := make(chan string, 1)
	reg.Register("test.verify", func(_ context.Context, args map[string]any) (any, error) {
		val := args["data"]
		if val == nil {
			val = args["value"]
		}
		verified <- val.(string)
		return nil, nil
	})

	script := `
def on_event(event):
    test.verify(value = event["foo"])

def broken_schedule():
    return None

clara.task(broken_schedule, schedule = "not-a-cron")
clara.task(on_event, trigger = "mock.active")
`
	if err := os.WriteFile(filepath.Join(dir, "multi-task.star"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}

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
	return []byte("def main():\n    return None\n")
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
	sup, reg := newTestSupervisor(t, dir)

	reg.Register("my.tool", func(_ context.Context, _ map[string]any) (any, error) {
		return nil, nil
	})

	if err := os.WriteFile(filepath.Join(dir, "test.star"), []byte(`
def main():
    my.tool()
clara.task(main, trigger="mock.event")
`), 0o600); err != nil {
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
