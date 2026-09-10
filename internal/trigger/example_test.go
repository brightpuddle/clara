package trigger_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/supervisor"
	"github.com/brightpuddle/clara/internal/trigger"
)

func TestExampleTriggers_EmailRouting(t *testing.T) {
	st, err := store.OpenMemory(zerolog.Nop())
	if err != nil {
		t.Fatalf("failed to open memory store: %v", err)
	}
	defer st.Close()

	bus := supervisor.NewEventBus()
	runner := trigger.NewRunner(5 * time.Second)
	mgr := trigger.NewManager(runner, bus, st)

	examplePath, err := filepath.Abs("../../examples/triggers/email_routing.yaml")
	if err != nil {
		t.Fatalf("failed to resolve path: %v", err)
	}

	if err := mgr.LoadFromFile(examplePath); err != nil {
		t.Fatalf("failed to load example triggers: %v", err)
	}

	urgentTrig, ok := mgr.Get("urgent-incident-email")
	if !ok {
		t.Fatalf("expected urgent-incident-email trigger to be loaded")
	}
	if urgentTrig.Type != trigger.TypeEvent {
		t.Errorf("expected event trigger type, got %s", urgentTrig.Type)
	}

	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(examplePath)))
	urgentTrig.Action.Dir = repoRoot
	_ = mgr.Register(*urgentTrig)

	// Test non-matching event (different mailbox)
	nonMatchEvent := map[string]any{
		"type": "email.received",
		"data": map[string]any{
			"mailbox": "info@brightpuddle.com",
			"subject": "URGENT: General question",
		},
	}
	matched, err := mgr.Test("urgent-incident-email", nonMatchEvent)
	if err != nil {
		t.Fatalf("unexpected error during test: %v", err)
	}
	if matched {
		t.Errorf("expected non-match for info mailbox")
	}

	// Test matching event (ops mailbox + CRITICAL subject)
	matchEvent := map[string]any{
		"type": "email.received",
		"data": map[string]any{
			"mailbox": "ops@brightpuddle.com",
			"subject": "CRITICAL: Database primary down",
			"body":    "Production outage detected on main db cluster.",
		},
	}
	matched, err = mgr.Test("urgent-incident-email", matchEvent)
	if err != nil {
		t.Fatalf("unexpected error during test: %v", err)
	}
	if !matched {
		t.Errorf("expected match for ops mailbox with critical subject")
	}

	// Run manually and check execution output
	record, err := mgr.Run(context.Background(), "urgent-incident-email", matchEvent)
	if err != nil {
		t.Fatalf("failed to run trigger: %v", err)
	}
	if record.Status != trigger.StatusSuccess {
		t.Errorf("expected status success, got %s (err: %s)", record.Status, record.Error)
	}
	if !strings.Contains(record.Stdout, "Starting Urgent Email Handler") {
		t.Errorf("expected script output to contain header, got: %s", record.Stdout)
	}
}
