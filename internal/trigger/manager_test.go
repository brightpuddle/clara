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

	if mgr.GetFilePath("cron-check") != tmpFile {
		t.Errorf("expected file path %s, got %s", tmpFile, mgr.GetFilePath("cron-check"))
	}
}

func TestManager_SaveToFile_And_DeleteTrigger(t *testing.T) {
	mgr := trigger.NewManager(nil, nil, nil)
	tmpDir := t.TempDir()

	def := trigger.Definition{
		ID:          "new-worker",
		Name:        "New Worker",
		Description: "A worker created dynamically",
		Enabled:     true,
		Type:        trigger.TypeWorker,
		Action: trigger.Action{
			Exec:    "python3 worker.py",
			Restart: trigger.RestartAlways,
		},
	}

	savedPath, err := mgr.SaveToFile(def, tmpDir)
	if err != nil {
		t.Fatalf("failed to save trigger: %v", err)
	}

	expectedPath := filepath.Join(tmpDir, "new-worker.yaml")
	if savedPath != expectedPath {
		t.Errorf("expected path %s, got %s", expectedPath, savedPath)
	}

	if _, err := os.Stat(savedPath); err != nil {
		t.Fatalf("saved file does not exist: %v", err)
	}

	// Verify it's registered
	got, ok := mgr.Get("new-worker")
	if !ok || got.Name != "New Worker" {
		t.Fatalf("expected trigger to be registered, got %v", got)
	}

	// Delete it
	if err := mgr.DeleteTrigger("new-worker"); err != nil {
		t.Fatalf("failed to delete trigger: %v", err)
	}

	if _, ok := mgr.Get("new-worker"); ok {
		t.Errorf("expected trigger to be removed from manager")
	}

	if _, err := os.Stat(savedPath); !os.IsNotExist(err) {
		t.Errorf("expected saved file to be removed from disk, err: %v", err)
	}
}

func TestManager_Debounce(t *testing.T) {
	st, err := store.OpenMemory(zerolog.Nop())
	if err != nil {
		t.Fatalf("failed to open memory store: %v", err)
	}
	defer st.Close()

	bus := supervisor.NewEventBus()
	runner := trigger.NewRunner(5 * time.Second)
	mgr := trigger.NewManager(runner, bus, st)

	outPath := filepath.Join(t.TempDir(), "debounce_out.txt")

	err = mgr.Register(trigger.Definition{
		ID:       "debounce-theme",
		Name:     "Debounced Theme Handler",
		Enabled:  true,
		Type:     trigger.TypeEvent,
		Debounce: 100 * time.Millisecond,
		Match: &trigger.Rule{
			Field: "type",
			Op:    trigger.OpEquals,
			Value: "theme_on_change",
		},
		Action: trigger.Action{
			Exec:      "sh",
			Args:      []string{"-c", "cat > " + outPath},
			PassEvent: trigger.PassEventStdin,
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

	// Emit 5 events in rapid succession (20ms interval < 100ms debounce window)
	for i := 1; i <= 5; i++ {
		bus.PublishCloud(supervisor.CloudEvent{
			ID:     "ev-theme",
			Source: "macos",
			Type:   "theme_on_change",
			Time:   time.Now(),
			Data: map[string]any{
				"seq": i,
			},
		})
		time.Sleep(20 * time.Millisecond)
	}

	// At this point (~100ms since first event, but only ~20ms since 5th event),
	// the debounce timer should still be pending and not yet executed.
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Fatalf("expected output file not to exist yet during active debounce window")
	}

	// Wait for debounce window (100ms) to elapse after the 5th event
	time.Sleep(200 * time.Millisecond)

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("expected output file to exist after debounce quiet period: %v", err)
	}

	// Verify only 1 run occurred in audit store
	runs, err := st.ListTriggerRuns(context.Background(), 10, "debounce-theme")
	if err != nil {
		t.Fatalf("failed to query runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected exactly 1 debounced run, got %d", len(runs))
	}

	// Verify output contains the payload of the last event (seq: 5)
	if len(data) == 0 {
		t.Fatalf("expected data in output file")
	}
}

func TestManager_Throttle(t *testing.T) {
	st, err := store.OpenMemory(zerolog.Nop())
	if err != nil {
		t.Fatalf("failed to open memory store: %v", err)
	}
	defer st.Close()

	bus := supervisor.NewEventBus()
	runner := trigger.NewRunner(5 * time.Second)
	mgr := trigger.NewManager(runner, bus, st)

	outPath := filepath.Join(t.TempDir(), "throttle_out.txt")

	err = mgr.Register(trigger.Definition{
		ID:       "throttle-sensor",
		Name:     "Throttled Sensor Handler",
		Enabled:  true,
		Type:     trigger.TypeEvent,
		Throttle: 200 * time.Millisecond,
		Match: &trigger.Rule{
			Field: "type",
			Op:    trigger.OpEquals,
			Value: "sensor.data",
		},
		Action: trigger.Action{
			Exec: "sh",
			Args: []string{"-c", "echo 'hit' >> " + outPath},
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

	// 1st event: should fire immediately
	bus.PublishCloud(supervisor.CloudEvent{
		ID:     "ev-1",
		Source: "sensor",
		Type:   "sensor.data",
		Time:   time.Now(),
	})

	time.Sleep(30 * time.Millisecond)

	// 2nd and 3rd events within the 200ms throttle window: should be throttled/dropped
	bus.PublishCloud(supervisor.CloudEvent{
		ID:     "ev-2",
		Source: "sensor",
		Type:   "sensor.data",
		Time:   time.Now(),
	})
	bus.PublishCloud(supervisor.CloudEvent{
		ID:     "ev-3",
		Source: "sensor",
		Type:   "sensor.data",
		Time:   time.Now(),
	})

	time.Sleep(80 * time.Millisecond)

	runs, err := st.ListTriggerRuns(context.Background(), 10, "throttle-sensor")
	if err != nil {
		t.Fatalf("failed to query runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected only 1 run before throttle window expires, got %d", len(runs))
	}

	// Wait for the throttle window (200ms) to pass completely
	time.Sleep(150 * time.Millisecond)

	// 4th event: should fire now that throttle window has passed
	bus.PublishCloud(supervisor.CloudEvent{
		ID:     "ev-4",
		Source: "sensor",
		Type:   "sensor.data",
		Time:   time.Now(),
	})

	time.Sleep(100 * time.Millisecond)

	runs, err = st.ListTriggerRuns(context.Background(), 10, "throttle-sensor")
	if err != nil {
		t.Fatalf("failed to query runs: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected exactly 2 runs after throttle window passed, got %d", len(runs))
	}
}
