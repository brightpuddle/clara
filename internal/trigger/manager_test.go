package trigger_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/supervisor"
	"github.com/brightpuddle/clara/internal/trigger"
)

func TestManager_EventMatchingAndExecution(t *testing.T) {
	st, err := store.OpenMemory(zerolog.Nop())
	if err != nil {
		t.Fatalf("failed to open memory store: %v", err)
	}
	defer st.Close()

	bus := supervisor.NewEventBus()
	runner := trigger.NewRunner(5 * time.Second)
	mgr := trigger.NewManager(runner, bus, st)

	outPath := filepath.Join(t.TempDir(), "output.txt")

	err = mgr.Register(trigger.Definition{
		ID:      "mail-trigger",
		Name:    "Mail Trigger",
		Enabled: true,
		Type:    trigger.TypeEvent,
		Match: &trigger.Rule{
			And: []*trigger.Rule{
				{
					Field: "type",
					Op:    trigger.OpEquals,
					Value: "email.received",
				},
				{
					Field: "data.subject",
					Op:    trigger.OpContains,
					Value: "ALERT",
				},
			},
		},
		Action: trigger.Action{
			Exec: "sh",
			Args: []string{"-c", "echo \"handled\" > " + outPath},
		},
	})
	if err != nil {
		t.Fatalf("failed to register trigger: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("failed to start manager: %v", err)
	}
	defer mgr.Stop()

	// 1. Publish non-matching event
	bus.PublishCloud(supervisor.CloudEvent{
		ID:     "ev-1",
		Source: "mail",
		Type:   "email.received",
		Time:   time.Now(),
		Data: map[string]any{
			"subject": "Normal newsletter",
		},
	})

	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Fatalf("expected output file to not exist for non-matching event")
	}

	// 2. Publish matching event
	bus.PublishCloud(supervisor.CloudEvent{
		ID:     "ev-2",
		Source: "mail",
		Type:   "email.received",
		Time:   time.Now(),
		Data: map[string]any{
			"subject": "[ALERT] High CPU",
		},
	})

	time.Sleep(300 * time.Millisecond)

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("expected output file to be written: %v", err)
	}
	if string(data) != "handled\n" {
		t.Errorf("unexpected output: %q", string(data))
	}

	// Verify audit store record
	runs, err := st.ListTriggerRuns(context.Background(), 10, "mail-trigger")
	if err != nil {
		t.Fatalf("failed to query runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run in store, got %d", len(runs))
	}
	if runs[0].Status != string(trigger.StatusSuccess) {
		t.Errorf("expected run status success, got %s", runs[0].Status)
	}
}

func TestManager_LoadFromFile(t *testing.T) {
	mgr := trigger.NewManager(nil, nil, nil)
	tmpFile := filepath.Join(t.TempDir(), "triggers.yaml")

	yamlContent := `
triggers:
  - id: "cron-check"
    name: "Cron Check"
    enabled: true
    type: "schedule"
    schedule: "0 0 * * *"
    action:
      exec: "/usr/bin/true"
  - id: "on-file"
    name: "On File"
    enabled: true
    type: "event"
    match:
      field: "type"
      op: "equals"
      value: "file.created"
    action:
      exec: "/usr/bin/true"
`
	if err := os.WriteFile(tmpFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write yaml: %v", err)
	}

	if err := mgr.LoadFromFile(tmpFile); err != nil {
		t.Fatalf("failed to load from file: %v", err)
	}

	list := mgr.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 triggers, got %d", len(list))
	}
}
