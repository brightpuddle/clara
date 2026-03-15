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
	reg := registry.New(zerolog.Nop())
	starlarkIt := interpreter.NewStarlark(reg, zerolog.Nop())
	sup := supervisor.New(tasksDir, reg, func(
		ctx context.Context,
		intent *orchestrator.Intent,
		runID string,
	) error {
		return starlarkIt.Execute(ctx, intent, "", interpreter.RunOptions{RunID: runID})
	}, zerolog.Nop())
	return sup, reg
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

	if err := os.WriteFile(filepath.Join(dir, "new.star"), validIntentStar("new-intent"), 0o600); err != nil {
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

func TestSupervisor_IgnoresNonStarFiles(t *testing.T) {
	dir := t.TempDir()
	sup, _ := newTestSupervisor(t, dir)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	if err := os.WriteFile(filepath.Join(dir, "intent.yaml"), []byte("id: ignored"), 0o600); err != nil {
		t.Fatal(err)
	}

	sup.Start(ctx) //nolint:errcheck

	if got := len(sup.ActiveIntents()); got != 0 {
		t.Fatalf("expected non-.star file to be ignored, got %d intent(s)", got)
	}
}

func TestSupervisor_LoadsExistingStarFiles(t *testing.T) {
	dir := t.TempDir()
	sup, _ := newTestSupervisor(t, dir)

	if err := os.WriteFile(filepath.Join(dir, "test.star"), validIntentStar("test-intent"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	sup.Start(ctx) //nolint:errcheck

	if len(sup.ActiveIntents()) == 0 {
		t.Fatal("expected .star intent to be loaded from existing files")
	}
}

func TestSupervisor_OnDemandIntentsDoNotAutoStart(t *testing.T) {
	dir := t.TempDir()
	sup, _ := newTestSupervisor(t, dir)

	if err := os.WriteFile(filepath.Join(dir, "test.star"), validIntentStar("test-intent"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	sup.Start(ctx) //nolint:errcheck

	infos := sup.IntentInfos()
	if len(infos) != 1 {
		t.Fatalf("expected one installed intent, got %d", len(infos))
	}
	if infos[0].Active {
		t.Fatal("expected on_demand intent to remain inactive until triggered")
	}
}

func TestSupervisor_WorkerIntentsAutoStart(t *testing.T) {
	dir := t.TempDir()
	sup, _ := newTestSupervisor(t, dir)

	worker := []byte("init(id = \"worker\", mode = \"worker\", interval = \"1h\")\n\ndef main():\n    return None\n")
	if err := os.WriteFile(filepath.Join(dir, "worker.star"), worker, 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		sup.Start(ctx) //nolint:errcheck
	}()
	time.Sleep(150 * time.Millisecond)

	infos := sup.IntentInfos()
	if len(infos) != 1 {
		t.Fatalf("expected one installed intent, got %d", len(infos))
	}
	if !infos[0].Active {
		t.Fatal("expected worker intent to auto-start")
	}
	cancel()
	<-done
}

func TestSupervisor_StartIntentRejectsOnDemand(t *testing.T) {
	dir := t.TempDir()
	sup, _ := newTestSupervisor(t, dir)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go sup.Start(ctx) //nolint:errcheck
	time.Sleep(100 * time.Millisecond)

	if err := os.WriteFile(filepath.Join(dir, "test.star"), validIntentStar("test-intent"), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(sup.IntentInfos()) == 1 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	if err := sup.StartIntent("test-intent"); err == nil {
		t.Fatal("expected StartIntent to reject on_demand intent")
	}
}
