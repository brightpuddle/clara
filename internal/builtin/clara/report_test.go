package clara

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/supervisor"
	"github.com/brightpuddle/clara/internal/trigger"
)

func TestRunReport_EndToEnd(t *testing.T) {
	st, err := store.OpenMemory(zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	mgr := trigger.NewManager(trigger.NewRunner(5*time.Second), supervisor.NewEventBus(), st)
	svc := &Service{db: st, triggerMgr: mgr}

	// The script reports through the same path as `clara tool call run.report`,
	// with the run id arriving on the context.
	script := `clara_run=$CLARA_RUN_ID; echo $clara_run > "$OUT"; while [ ! -f "$OUT.go" ]; do sleep 0.02; done`
	dir := t.TempDir()
	out := dir + "/id"
	if err := mgr.Register(trigger.Definition{
		ID: "r", Name: "r", Enabled: true, Type: trigger.TypeManual,
		Action: trigger.Action{Exec: "sh", Args: []string{"-c", script}, Env: map[string]string{"OUT": out}},
	}); err != nil {
		t.Fatal(err)
	}

	done := make(chan *trigger.RunRecord, 1)
	go func() {
		rec, _ := mgr.Run(context.Background(), "r", nil)
		done <- rec
	}()

	var runID string
	for i := 0; i < 150 && runID == ""; i++ {
		if b, err := readTrim(out); err == nil {
			runID = b
		}
		time.Sleep(20 * time.Millisecond)
	}
	if runID == "" {
		t.Fatal("script did not start")
	}

	ctx := context.WithValue(context.Background(), registry.RunIDContextKey, runID)
	if _, err := svc.handleRunReport(ctx, map[string]any{
		"outcome": "partial", "summary": "9 of 10 filed",
		"counts":    map[string]any{"seen": float64(10)},
		"anomalies": []any{"1 left for review"},
	}); err != nil {
		t.Fatalf("report: %v", err)
	}
	if _, err := svc.handleRunReport(ctx, map[string]any{"outcome": "bogus", "summary": "x"}); err == nil {
		t.Fatal("expected invalid outcome to be rejected")
	}
	if _, err := svc.handleRunReport(context.Background(), map[string]any{
		"outcome": "ok", "summary": "x"}); err == nil {
		t.Fatal("expected error without run id")
	}

	touch(t, out+".go")
	rec := <-done
	got, _ := st.GetTriggerRun(context.Background(), rec.ID)
	if got == nil || got.Receipt == "" || rec.Receipt == nil || rec.Receipt.Outcome != "partial" ||
		rec.Receipt.Counts["seen"] != 10 {
		t.Fatalf("receipt not persisted: %+v / %+v", rec.Receipt, got)
	}
}

func readTrim(p string) (string, error) {
	b, err := os.ReadFile(p)
	return strings.TrimSpace(string(b)), err
}

func touch(t *testing.T, p string) {
	t.Helper()
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}
