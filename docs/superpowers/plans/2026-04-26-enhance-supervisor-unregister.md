# Enhance Supervisor for Intent Unregistration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `UnregisterIntent(id string)` in `Supervisor` to allow unloading plugins by removing their intents and stopping associated workers.

**Architecture:** `UnregisterIntent` will acquire the supervisor's lock, retrieve the `managedIntent`, cancel all its running tasks, and remove it from the `intents` map.

**Tech Stack:** Go

---

### Task 1: Write Failing Test for UnregisterIntent

**Files:**
- Test: `internal/supervisor/supervisor_test.go`

- [ ] **Step 1: Write the failing test**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/supervisor/supervisor_test.go -run TestSupervisor_UnregisterIntent`
Expected: FAIL with "sup.UnregisterIntent undefined"

---

### Task 2: Implement UnregisterIntent

**Files:**
- Modify: `internal/supervisor/supervisor.go`

- [ ] **Step 1: Implement the minimal code to make the test pass**

```go
// UnregisterIntent removes an intent from the supervisor and stops its tasks.
func (s *Supervisor) UnregisterIntent(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	managed, ok := s.intents[id]
	if !ok {
		return errors.Newf("intent %q not found", id)
	}

	for _, cancel := range managed.cancels {
		cancel()
	}
	managed.cancels = nil
	managed.activeTasks = 0
	managed.active = false

	delete(s.intents, id)
	return nil
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test -v ./internal/supervisor/...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/supervisor/supervisor.go internal/supervisor/supervisor_test.go
git commit -m "feat(supervisor): add UnregisterIntent for plugin unloading"
```
