package trigger_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/supervisor"
	"github.com/brightpuddle/clara/internal/trigger"
)

func TestIntegration_EventTrigger_HelloWorld(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "clara.db")
	st, err := store.Open(dbPath, zerolog.Nop())
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	bus := supervisor.NewEventBus()
	runner := trigger.NewRunner(5 * time.Second)
	mgr := trigger.NewManager(runner, bus, st)

	scriptPath := filepath.Join(tempDir, "event_script.sh")
	scriptContent := `#!/bin/sh
echo "hello world from event trigger"
if [ -n "$CLARA_EVENT_TYPE" ]; then
    echo "received event type: $CLARA_EVENT_TYPE"
fi
`
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	def := trigger.Definition{
		ID:          "e2e-event-hello",
		Name:        "E2E Event Hello",
		Description: "Functional test for event trigger",
		Enabled:     true,
		Type:        trigger.TypeEvent,
		Match: &trigger.Rule{
			And: []*trigger.Rule{
				{
					Field: "type",
					Op:    trigger.OpEquals,
					Value: "user.greeted",
				},
				{
					Field: "data.recipient",
					Op:    trigger.OpEquals,
					Value: "Clara",
				},
			},
		},
		Action: trigger.Action{
			Exec:      scriptPath,
			PassEvent: trigger.PassEventStdin,
			Timeout:   3 * time.Second,
		},
	}

	if err := mgr.Register(def); err != nil {
		t.Fatalf("failed to register event trigger: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("failed to start manager: %v", err)
	}
	defer mgr.Stop()

	// 1. Emit non-matching event
	bus.PublishCloud(supervisor.CloudEvent{
		ID:     "ev-nonmatch",
		Source: "test",
		Type:   "user.greeted",
		Time:   time.Now(),
		Data: map[string]any{
			"recipient": "Other",
		},
	})

	time.Sleep(150 * time.Millisecond)
	runs, err := st.ListTriggerRuns(context.Background(), 10, "e2e-event-hello")
	if err != nil {
		t.Fatalf("failed to list trigger runs: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("expected 0 runs for non-matching event, got %d", len(runs))
	}

	// 2. Emit matching event
	bus.PublishCloud(supervisor.CloudEvent{
		ID:     "ev-match-1",
		Source: "test",
		Type:   "user.greeted",
		Time:   time.Now(),
		Data: map[string]any{
			"recipient": "Clara",
		},
	})

	// Allow execution and store recording to complete
	for i := 0; i < 40; i++ {
		time.Sleep(50 * time.Millisecond)
		runs, err = st.ListTriggerRuns(context.Background(), 10, "e2e-event-hello")
		if err == nil && len(runs) == 1 {
			break
		}
	}
	if err != nil {
		t.Fatalf("failed to list trigger runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run for matching event, got %d", len(runs))
	}

	run := runs[0]
	if run.Status != string(trigger.StatusSuccess) {
		t.Errorf("expected status %q, got %q (err: %s)", trigger.StatusSuccess, run.Status, run.Error)
	}
	if run.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", run.ExitCode)
	}
	if !strings.Contains(run.Stdout, "hello world from event trigger") {
		t.Errorf("expected stdout to contain greeting, got: %q", run.Stdout)
	}
	if !strings.Contains(run.Stdout, "received event type: user.greeted") {
		t.Errorf("expected stdout to contain event type env var, got: %q", run.Stdout)
	}
}

func TestIntegration_ScheduleTrigger_HelloWorld(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "clara.db")
	st, err := store.Open(dbPath, zerolog.Nop())
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	bus := supervisor.NewEventBus()
	runner := trigger.NewRunner(5 * time.Second)
	mgr := trigger.NewManager(runner, bus, st)

	scriptPath := filepath.Join(tempDir, "schedule_script.sh")
	scriptContent := `#!/bin/sh
echo "hello world from schedule trigger"
`
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	def := trigger.Definition{
		ID:          "e2e-schedule-hello",
		Name:        "E2E Schedule Hello",
		Description: "Functional test for schedule trigger",
		Enabled:     true,
		Type:        trigger.TypeSchedule,
		Schedule:    "@every 1s",
		Action: trigger.Action{
			Exec:    scriptPath,
			Timeout: 2 * time.Second,
		},
	}

	if err := mgr.Register(def); err != nil {
		t.Fatalf("failed to register schedule trigger: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("failed to start manager: %v", err)
	}
	defer mgr.Stop()

	// Wait for at least 2 schedule ticks (~2.2s)
	time.Sleep(2200 * time.Millisecond)

	runs, err := st.ListTriggerRuns(context.Background(), 10, "e2e-schedule-hello")
	if err != nil {
		t.Fatalf("failed to list trigger runs: %v", err)
	}
	if len(runs) < 2 {
		t.Fatalf("expected at least 2 runs from schedule trigger, got %d", len(runs))
	}

	for i, r := range runs {
		if r.Status != string(trigger.StatusSuccess) {
			t.Errorf("run %d: expected status %q, got %q", i, trigger.StatusSuccess, r.Status)
		}
		if !strings.Contains(r.Stdout, "hello world from schedule trigger") {
			t.Errorf("run %d: expected stdout to contain greeting, got: %q", i, r.Stdout)
		}
	}
}

func TestIntegration_WorkerTrigger_HelloWorld_WithRestarts(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "clara.db")
	st, err := store.Open(dbPath, zerolog.Nop())
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	bus := supervisor.NewEventBus()
	runner := trigger.NewRunner(5 * time.Second)
	mgr := trigger.NewManager(runner, bus, st)

	counterFile := filepath.Join(tempDir, "worker_count.txt")
	scriptPath := filepath.Join(tempDir, "worker_script.sh")
	scriptContent := fmt.Sprintf(`#!/bin/sh
echo "hello world from worker"
echo "run" >> %s
exit 1
`, counterFile)

	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755); err != nil {
		t.Fatalf("failed to write worker script: %v", err)
	}

	def := trigger.Definition{
		ID:          "e2e-worker-hello",
		Name:        "E2E Worker Hello",
		Description: "Functional test for worker trigger with restart policy",
		Enabled:     true,
		Type:        trigger.TypeWorker,
		Action: trigger.Action{
			Exec:         scriptPath,
			Restart:      trigger.RestartOnFailure,
			RestartDelay: 50 * time.Millisecond,
			MaxRestarts:  3,
			Timeout:      2 * time.Second,
		},
	}

	if err := mgr.Register(def); err != nil {
		t.Fatalf("failed to register worker trigger: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("failed to start manager: %v", err)
	}
	defer mgr.Stop()

	// Wait for worker restarts to complete (3 total runs for MaxRestarts: 3)
	var runs []store.TriggerRunRecord
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var err error
		runs, err = st.ListTriggerRuns(context.Background(), 10, "e2e-worker-hello")
		if err == nil && len(runs) >= 3 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if len(runs) != 3 {
		t.Fatalf("expected 3 worker runs (MaxRestarts: 3), got %d", len(runs))
	}

	for i, r := range runs {
		if r.Status != string(trigger.StatusFailure) {
			t.Errorf("run %d: expected status %q, got %q", i, trigger.StatusFailure, r.Status)
		}
		if r.ExitCode != 1 {
			t.Errorf("run %d: expected exit code 1, got %d", i, r.ExitCode)
		}
		if !strings.Contains(r.Stdout, "hello world from worker") {
			t.Errorf("run %d: expected stdout to contain greeting, got: %q", i, r.Stdout)
		}
	}
}

func TestIntegration_ManualTrigger(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "clara.db")
	st, err := store.Open(dbPath, zerolog.Nop())
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	bus := supervisor.NewEventBus()
	runner := trigger.NewRunner(5 * time.Second)
	mgr := trigger.NewManager(runner, bus, st)

	scriptPath := filepath.Join(tempDir, "manual_script.sh")
	scriptContent := `#!/bin/sh
echo "hello from manual trigger: $CLARA_TRIGGER_ID"
`
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755); err != nil {
		t.Fatalf("failed to write manual script: %v", err)
	}

	def := trigger.Definition{
		ID:          "manual-task-1",
		Name:        "Manual Task 1",
		Description: "A script that only runs on manual invocation",
		Enabled:     true,
		Type:        trigger.TypeManual,
		Action: trigger.Action{
			Exec: scriptPath,
		},
	}

	if err := mgr.Register(def); err != nil {
		t.Fatalf("failed to register manual trigger: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("failed to start manager: %v", err)
	}
	defer mgr.Stop()

	// 1. Emit random events to ensure manual trigger never auto-fires
	bus.PublishCloud(supervisor.CloudEvent{
		ID:     "ev-1",
		Source: "test",
		Type:   "custom.event",
		Time:   time.Now(),
	})
	time.Sleep(100 * time.Millisecond)

	runs, err := st.ListTriggerRuns(context.Background(), 10, "manual-task-1")
	if err != nil {
		t.Fatalf("failed to list runs: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("expected 0 runs before manual execution, got %d", len(runs))
	}

	// 2. Explicitly execute manual trigger via mgr.Run
	runRecord, err := mgr.Run(ctx, "manual-task-1", map[string]any{"user": "nathan"})
	if err != nil {
		t.Fatalf("failed to run manual trigger: %v", err)
	}
	if runRecord.Status != trigger.StatusSuccess {
		t.Errorf("expected success, got %s", runRecord.Status)
	}
	if !strings.Contains(runRecord.Stdout, "hello from manual trigger: manual-task-1") {
		t.Errorf("unexpected stdout: %s", runRecord.Stdout)
	}

	// 3. Verify audit record stored
	runs, err = st.ListTriggerRuns(context.Background(), 10, "manual-task-1")
	if err != nil {
		t.Fatalf("failed to list runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run in store, got %d", len(runs))
	}
	if runs[0].TriggerType != string(trigger.TypeManual) {
		t.Errorf("expected trigger_type %s, got %s", trigger.TypeManual, runs[0].TriggerType)
	}
}

func TestIntegration_AllTriggers_CombinedTaskDirectory(t *testing.T) {
	tempDir := t.TempDir()
	taskDir := filepath.Join(tempDir, "tasks")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("failed to create task dir: %v", err)
	}

	dbPath := filepath.Join(tempDir, "clara.db")
	st, err := store.Open(dbPath, zerolog.Nop())
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	bus := supervisor.NewEventBus()
	runner := trigger.NewRunner(5 * time.Second)
	mgr := trigger.NewManager(runner, bus, st)

	// Create scripts for all 4
	eventScript := filepath.Join(tempDir, "event.sh")
	_ = os.WriteFile(eventScript, []byte("#!/bin/sh\necho \"all-triggers-event: $CLARA_TRIGGER_ID\"\n"), 0o755)

	scheduleScript := filepath.Join(tempDir, "schedule.sh")
	_ = os.WriteFile(scheduleScript, []byte("#!/bin/sh\necho \"all-triggers-schedule: $CLARA_TRIGGER_ID\"\n"), 0o755)

	workerScript := filepath.Join(tempDir, "worker.sh")
	_ = os.WriteFile(workerScript, []byte("#!/bin/sh\necho \"all-triggers-worker: $CLARA_TRIGGER_ID\"\nexit 0\n"), 0o755)

	manualScript := filepath.Join(tempDir, "manual.sh")
	_ = os.WriteFile(manualScript, []byte("#!/bin/sh\necho \"all-triggers-manual: $CLARA_TRIGGER_ID\"\n"), 0o755)

	// Write YAML definitions into tasks directory
	eventYAML := fmt.Sprintf(`
id: task-dir-event
name: Task Dir Event
enabled: true
type: event
match:
  field: type
  op: equals
  value: system.ping
action:
  exec: %s
`, eventScript)

	scheduleYAML := fmt.Sprintf(`
id: task-dir-schedule
name: Task Dir Schedule
enabled: true
type: schedule
schedule: "@every 1s"
action:
  exec: %s
`, scheduleScript)

	workerYAML := fmt.Sprintf(`
id: task-dir-worker
name: Task Dir Worker
enabled: true
type: worker
action:
  exec: %s
  restart: never
`, workerScript)

	manualYAML := fmt.Sprintf(`
id: task-dir-manual
name: Task Dir Manual
enabled: true
type: manual
action:
  exec: %s
`, manualScript)

	if err := os.WriteFile(filepath.Join(taskDir, "01-event.yaml"), []byte(eventYAML), 0o644); err != nil {
		t.Fatalf("failed to write event YAML: %v", err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "02-schedule.yaml"), []byte(scheduleYAML), 0o644); err != nil {
		t.Fatalf("failed to write schedule YAML: %v", err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "03-worker.yaml"), []byte(workerYAML), 0o644); err != nil {
		t.Fatalf("failed to write worker YAML: %v", err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "04-manual.yaml"), []byte(manualYAML), 0o644); err != nil {
		t.Fatalf("failed to write manual YAML: %v", err)
	}

	// Load directory
	if err := mgr.LoadFromDir(taskDir); err != nil {
		t.Fatalf("failed to load from task dir: %v", err)
	}

	list := mgr.List()
	if len(list) != 4 {
		t.Fatalf("expected 4 triggers loaded, got %d", len(list))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("failed to start manager: %v", err)
	}
	defer mgr.Stop()

	// Emit event for event trigger
	bus.PublishCloud(supervisor.CloudEvent{
		ID:     "ping-1",
		Source: "test",
		Type:   "system.ping",
		Time:   time.Now(),
	})

	// Run manual trigger
	manRec, err := mgr.Run(ctx, "task-dir-manual", nil)
	if err != nil {
		t.Fatalf("failed to run manual trigger: %v", err)
	}
	if manRec.Status != trigger.StatusSuccess {
		t.Errorf("manual trigger expected success, got %s", manRec.Status)
	}

	// Wait for event, schedule tick, and worker execution
	time.Sleep(1300 * time.Millisecond)

	// Verify event run
	eventRuns, err := st.ListTriggerRuns(context.Background(), 5, "task-dir-event")
	if err != nil || len(eventRuns) == 0 {
		t.Fatalf("expected event run, got %v (err: %v)", eventRuns, err)
	}
	if !strings.Contains(eventRuns[0].Stdout, "all-triggers-event: task-dir-event") {
		t.Errorf("unexpected event stdout: %s", eventRuns[0].Stdout)
	}

	// Verify schedule run
	schedRuns, err := st.ListTriggerRuns(context.Background(), 5, "task-dir-schedule")
	if err != nil || len(schedRuns) == 0 {
		t.Fatalf("expected schedule run, got %v (err: %v)", schedRuns, err)
	}
	if !strings.Contains(schedRuns[0].Stdout, "all-triggers-schedule: task-dir-schedule") {
		t.Errorf("unexpected schedule stdout: %s", schedRuns[0].Stdout)
	}

	// Verify worker run
	workerRuns, err := st.ListTriggerRuns(context.Background(), 5, "task-dir-worker")
	if err != nil || len(workerRuns) == 0 {
		t.Fatalf("expected worker run, got %v (err: %v)", workerRuns, err)
	}
	if !strings.Contains(workerRuns[0].Stdout, "all-triggers-worker: task-dir-worker") {
		t.Errorf("unexpected worker stdout: %s", workerRuns[0].Stdout)
	}

	// Verify manual run
	manRuns, err := st.ListTriggerRuns(context.Background(), 5, "task-dir-manual")
	if err != nil || len(manRuns) == 0 {
		t.Fatalf("expected manual run, got %v (err: %v)", manRuns, err)
	}
	if !strings.Contains(manRuns[0].Stdout, "all-triggers-manual: task-dir-manual") {
		t.Errorf("unexpected manual stdout: %s", manRuns[0].Stdout)
	}
}
