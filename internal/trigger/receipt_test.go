package trigger_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/trigger"
)

func TestReceipt_Validate(t *testing.T) {
	ok := trigger.Receipt{Outcome: "partial", Summary: "did some"}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid receipt rejected: %v", err)
	}
	for name, r := range map[string]trigger.Receipt{
		"bad outcome":   {Outcome: "great", Summary: "x"},
		"empty outcome": {Summary: "x"},
		"no summary":    {Outcome: "ok"},
	} {
		if err := r.Validate(); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestMechanicalReceipt(t *testing.T) {
	calls := []store.ToolCallRecord{
		{ToolName: "db.query"}, {ToolName: "db.query"},
		{ToolName: "mail.move", Error: "boom"},
	}
	rec := &trigger.RunRecord{
		Status: trigger.StatusSuccess, DurationMs: 1500,
	}
	r := trigger.MechanicalReceipt(rec, calls)
	if !r.Mechanical || r.Outcome != "ok" {
		t.Fatalf("unexpected: %+v", r)
	}
	if r.Counts["tool_calls"] != 3 || r.Counts["tool_errors"] != 1 {
		t.Fatalf("counts: %+v", r.Counts)
	}
	for _, want := range []string{"db.query", "mail.move", "1.5s"} {
		if !strings.Contains(r.Summary, want) {
			t.Errorf("summary %q missing %q", r.Summary, want)
		}
	}

	rec.Status = trigger.StatusTimeout
	rec.Error = "execution timed out after 1m0s"
	if r := trigger.MechanicalReceipt(rec, nil); r.Outcome != "failed" ||
		!strings.Contains(r.Summary, "timed out") {
		t.Fatalf("timeout receipt: %+v", r)
	}
}

func TestRunner_Report_ActiveRunOnly(t *testing.T) {
	runner := trigger.NewRunner(5 * time.Second)
	if err := runner.Report("run-nope", trigger.Receipt{Outcome: "ok", Summary: "x"}); err == nil {
		t.Fatal("expected error for unknown run")
	}
}

func TestRunner_ParentRunFromContext(t *testing.T) {
	runner := trigger.NewRunner(5 * time.Second)
	ctx := context.WithValue(context.Background(), registry.RunIDContextKey, "run-parent")
	def := trigger.Definition{ID: "t", Type: trigger.TypeManual,
		Action: trigger.Action{Exec: "sh", Args: []string{"-c", "true"}}}
	rec := runner.Execute(ctx, def, map[string]any{"id": "evt-1", "type": "x"})
	if rec.ParentRunID != "run-parent" || rec.EventID != "evt-1" {
		t.Fatalf("links: parent=%q event=%q", rec.ParentRunID, rec.EventID)
	}
}

func TestRunner_ReceiptCollectedFromScript(t *testing.T) {
	runner := trigger.NewRunner(5 * time.Second)
	var runID string
	// The script can't call Report itself in a unit test, so a goroutine
	// polls the env-provided run id via a file the script writes.
	dir := t.TempDir()
	def := trigger.Definition{ID: "t", Type: trigger.TypeManual, Action: trigger.Action{
		Exec: "sh",
		Args: []string{"-c", `echo $CLARA_RUN_ID > ` + dir + `/id; while [ ! -f ` + dir + `/go ]; do sleep 0.02; done`},
	}}
	done := make(chan *trigger.RunRecord, 1)
	go func() { done <- runner.Execute(context.Background(), def, nil) }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := readFile(dir + "/id"); err == nil && len(b) > 0 {
			runID = strings.TrimSpace(string(b))
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if runID == "" {
		t.Fatal("script never wrote run id")
	}
	want := trigger.Receipt{Outcome: "noop", Summary: "nothing to do", SessionID: "ses-1"}
	if err := runner.Report(runID, want); err != nil {
		t.Fatalf("report: %v", err)
	}
	writeFile(t, dir+"/go")
	rec := <-done
	if rec.Receipt == nil || rec.Receipt.Outcome != "noop" || rec.SessionID != "ses-1" {
		t.Fatalf("receipt not collected: %+v", rec)
	}
}

func readFile(p string) ([]byte, error) { return os.ReadFile(p) }

func writeFile(t *testing.T, p string) {
	t.Helper()
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRunner_CancelledByParentContext(t *testing.T) {
	runner := trigger.NewRunner(5 * time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	def := trigger.Definition{ID: "t", Type: trigger.TypeWorker,
		Action: trigger.Action{Exec: "sh", Args: []string{"-c", "sleep 30"}}}
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	rec := runner.Execute(ctx, def, nil)
	if rec.Status != trigger.StatusCancelled {
		t.Fatalf("status = %s, want cancelled", rec.Status)
	}
}

func TestRunner_ActiveListsRunningRuns(t *testing.T) {
	runner := trigger.NewRunner(5 * time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	def := trigger.Definition{ID: "w", Type: trigger.TypeWorker,
		Action: trigger.Action{Exec: "sh", Args: []string{"-c", "sleep 30"}}}
	done := make(chan struct{})
	go func() { runner.Execute(ctx, def, nil); close(done) }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && runner.Active()["w"].IsZero() {
		time.Sleep(20 * time.Millisecond)
	}
	if runner.Active()["w"].IsZero() {
		t.Fatal("worker not reported active")
	}
	cancel()
	<-done
	if !runner.Active()["w"].IsZero() {
		t.Fatal("worker still active after exit")
	}
}
