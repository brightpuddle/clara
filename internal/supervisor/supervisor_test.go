package supervisor_test

import (
	"context"
	"encoding/json"
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
	reg := registry.New(zerolog.Nop())
	it := interpreter.New(reg, zerolog.Nop())
	sup := supervisor.New(tasksDir, reg, it, zerolog.Nop())
	return sup, reg
}

func validIntentJSON(t *testing.T) []byte {
	t.Helper()
	intent := orchestrator.Intent{
		ID:           "test-intent",
		InitialState: "START",
		States: map[string]orchestrator.State{
			"START": {Terminal: true},
		},
	}
	data, err := json.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	return data
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

	// Write a valid Intent JSON file (no LLM needed).
	data := validIntentJSON(t)
	if err := os.WriteFile(filepath.Join(dir, "test.md"), data, 0o600); err != nil {
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

func TestSupervisor_WatchesForNewFiles(t *testing.T) {
	dir := t.TempDir()
	sup, _ := newTestSupervisor(t, dir)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	started := make(chan struct{})
	go func() {
		close(started)
		sup.Start(ctx) //nolint:errcheck
	}()
	<-started

	// Give the watcher a moment to initialize.
	time.Sleep(100 * time.Millisecond)

	// Write a new task file.
	data := validIntentJSON(t)
	if err := os.WriteFile(filepath.Join(dir, "new.md"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	// Wait for the supervisor to pick it up.
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(sup.ActiveIntents()) > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Error("expected intent to be loaded after file was written")
}

func TestSupervisor_LLMFallback_WhenNotJSON(t *testing.T) {
	dir := t.TempDir()
	sup, reg := newTestSupervisor(t, dir)

	// Register the LLM tool that returns valid Intent JSON.
	data := validIntentJSON(t)
	reg.Register(supervisor.LLMTool, func(_ context.Context, args map[string]any) (any, error) {
		return string(data), nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Write a Markdown intent file (not JSON).
	mdContent := "# My Task\nSync reminders with taskwarrior."
	if err := os.WriteFile(filepath.Join(dir, "intent.md"), []byte(mdContent), 0o600); err != nil {
		t.Fatal(err)
	}

	sup.Start(ctx) //nolint:errcheck

	intents := sup.ActiveIntents()
	if len(intents) == 0 {
		t.Error("expected intent from LLM conversion to be loaded")
	}
}
