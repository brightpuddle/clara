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

func validBlueprintJSON(t *testing.T) []byte {
	t.Helper()
	bp := orchestrator.Blueprint{
		ID:           "test-bp",
		InitialState: "START",
		States: map[string]orchestrator.State{
			"START": {Terminal: true},
		},
	}
	data, err := json.Marshal(bp)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSupervisor_ValidateBlueprint_Valid(t *testing.T) {
	sup, reg := newTestSupervisor(t, t.TempDir())

	reg.Register("my.tool", func(_ context.Context, _ map[string]any) (any, error) {
		return nil, nil
	})

	bp := &orchestrator.Blueprint{
		ID:           "valid",
		InitialState: "RUN",
		States: map[string]orchestrator.State{
			"RUN": {Action: "my.tool", Terminal: true},
		},
	}
	if err := sup.ValidateBlueprint(bp); err != nil {
		t.Fatalf("expected valid blueprint, got: %v", err)
	}
}

func TestSupervisor_ValidateBlueprint_UnregisteredTool(t *testing.T) {
	sup, _ := newTestSupervisor(t, t.TempDir())

	bp := &orchestrator.Blueprint{
		ID:           "unregistered",
		InitialState: "RUN",
		States: map[string]orchestrator.State{
			"RUN": {Action: "nonexistent.tool", Terminal: true},
		},
	}
	err := sup.ValidateBlueprint(bp)
	if err == nil {
		t.Fatal("expected error for unregistered tool")
	}
}

func TestSupervisor_ValidateBlueprint_InvalidStructure(t *testing.T) {
	sup, _ := newTestSupervisor(t, t.TempDir())

	bp := &orchestrator.Blueprint{
		ID:    "", // missing required field
		States: map[string]orchestrator.State{},
	}
	if err := sup.ValidateBlueprint(bp); err == nil {
		t.Fatal("expected error for invalid blueprint structure")
	}
}

func TestSupervisor_LoadsExistingFiles(t *testing.T) {
	dir := t.TempDir()
	sup, _ := newTestSupervisor(t, dir)

	// Write a valid Blueprint JSON file (no LLM needed).
	data := validBlueprintJSON(t)
	if err := os.WriteFile(filepath.Join(dir, "test.md"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Start runs until ctx is cancelled; it should load existing files first.
	sup.Start(ctx) //nolint:errcheck

	bps := sup.ActiveBlueprints()
	if len(bps) == 0 {
		t.Error("expected at least one blueprint to be loaded from existing files")
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
	data := validBlueprintJSON(t)
	if err := os.WriteFile(filepath.Join(dir, "new.md"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	// Wait for the supervisor to pick it up.
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(sup.ActiveBlueprints()) > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Error("expected blueprint to be loaded after file was written")
}

func TestSupervisor_LLMFallback_WhenNotJSON(t *testing.T) {
	dir := t.TempDir()
	sup, reg := newTestSupervisor(t, dir)

	// Register the LLM tool that returns valid Blueprint JSON.
	data := validBlueprintJSON(t)
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

	bps := sup.ActiveBlueprints()
	if len(bps) == 0 {
		t.Error("expected blueprint from LLM conversion to be loaded")
	}
}
