package supervisor_test

import (
	"context"
	"os"
	"testing"
	"time"

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
	sup := supervisor.New(tasksDir, reg, func(
		ctx context.Context,
		intent *orchestrator.Intent,
		runID string,
		entrypoint string,
		args any,
	) error {
		// Mock runner simply calls a tool to signal it ran
		if intent.WorkflowType == orchestrator.WorkflowTypeNative {
			_, err := reg.Call(ctx, "test.verify", map[string]any{"data": args})
			return err
		}
		return nil
	}, log)
	return sup, reg
}

func TestSupervisor_EventTrigger(t *testing.T) {
	dir := t.TempDir()
	sup, reg := newTestSupervisor(t, dir)

	intent := &orchestrator.Intent{
		ID:           "event_intent",
		WorkflowType: orchestrator.WorkflowTypeNative,
		Script:       "/tmp/mock-plugin",
		Tasks: []orchestrator.Task{
			{
				Handler: "on_event",
				Mode:    orchestrator.IntentModeEvent,
				Trigger: "mock.test_event",
			},
		},
	}

	verified := make(chan any, 1)
	reg.Register("test.verify", func(_ context.Context, args map[string]any) (any, error) {
		verified <- args["data"]
		return nil, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sup.Start(ctx) //nolint:errcheck

	if err := sup.RegisterIntent(intent.Script, intent); err != nil {
		t.Fatal(err)
	}

	// Wait for subscription to be active
	time.Sleep(100 * time.Millisecond)

	// Emit notification
	reg.EmitNotification("mock", "test_event", map[string]any{"foo": "bar"})

	select {
	case data := <-verified:
		params := data.(map[string]any)
		if params["foo"] != "bar" {
			t.Errorf("expected bar, got %v", params["foo"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for handler to be called")
	}
}

func TestSupervisor_WorkerRunsOnInterval(t *testing.T) {
	dir := t.TempDir()
	sup, reg := newTestSupervisor(t, dir)

	called := make(chan bool, 1)
	reg.Register("test.verify", func(_ context.Context, args map[string]any) (any, error) {
		select {
		case called <- true:
		default:
		}
		return nil, nil
	})

	intent := &orchestrator.Intent{
		ID:           "worker_intent",
		WorkflowType: orchestrator.WorkflowTypeNative,
		Script:       "/tmp/mock-plugin-worker",
		Tasks: []orchestrator.Task{
			{
				Handler:  "run",
				Mode:     orchestrator.IntentModeWorker,
				Interval: "100ms",
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sup.Start(ctx) //nolint:errcheck

	if err := sup.RegisterIntent(intent.Script, intent); err != nil {
		t.Fatal(err)
	}

	select {
	case <-called:
		// Good
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for worker task to run")
	}
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
	if _, ok := err.(*supervisor.SupervisorValidationError); !ok {
		t.Errorf("expected SupervisorValidationError, got %T", err)
	}
}

func TestSupervisor_UnregisterIntent(t *testing.T) {
	dir := t.TempDir()
	sup, reg := newTestSupervisor(t, dir)

	called := make(chan bool, 1)
	reg.Register("test.verify", func(_ context.Context, args map[string]any) (any, error) {
		select {
		case called <- true:
		default:
		}
		return nil, nil
	})

	intent := &orchestrator.Intent{
		ID:           "unregister_intent",
		WorkflowType: orchestrator.WorkflowTypeNative,
		Script:       "/tmp/mock-plugin-unregister",
		Tasks: []orchestrator.Task{
			{
				Handler:  "run",
				Mode:     orchestrator.IntentModeWorker,
				Interval: "100ms",
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sup.Start(ctx) //nolint:errcheck

	if err := sup.RegisterIntent(intent.Script, intent); err != nil {
		t.Fatal(err)
	}

	// Verify it runs
	select {
	case <-called:
		// Good
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for worker task to run before unregister")
	}

	// Unregister
	if err := sup.UnregisterIntent(intent.ID); err != nil {
		t.Fatalf("failed to unregister intent: %v", err)
	}

	// Verify it's gone from ActiveIntents
	active := sup.ActiveIntents()
	for _, a := range active {
		if a.ID == intent.ID {
			t.Errorf("intent %q still active after unregister", intent.ID)
		}
	}

	// Verify it doesn't run anymore
	// Clear the channel
	select {
	case <-called:
	default:
	}

	select {
	case <-called:
		t.Error("worker task still running after unregister")
	case <-time.After(500 * time.Millisecond):
		// Good, didn't run
	}
}
