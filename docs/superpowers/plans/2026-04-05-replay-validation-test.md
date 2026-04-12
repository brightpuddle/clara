# Replay Validation Test Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an integration test to verify Starlark replay logic for interactive TUI prompts.

**Architecture:** Use the `TUIIntegrationHarness` to execute a Starlark script with multiple interactive prompts, capturing history after the first interaction and ensuring it's correctly skipped during a replay.

**Tech Stack:** Go, Starlark, Bubbletea (TUI)

---

### Task 1: Add TestIntegration_Starlark_Replay to internal/tui/integration_test.go

**Files:**
- Modify: `internal/tui/integration_test.go`

- [ ] **Step 1: Implement the TestIntegration_Starlark_Replay function**

```go
func TestIntegration_Starlark_Replay(t *testing.T) {
	h := NewHarness(t)
	defer h.Close()

	// Script that calls notify_send_interactive twice
	script := `
def main():
    res1 = notify.send_interactive(
        prompt="First Prompt",
        options=["A", "B"]
    )
    res2 = notify.send_interactive(
        prompt="Second Prompt",
        options=["C", "D"]
    )
    return [res1, res2]
`

	var history []interpreter.ReplayEntry
	load := func(ctx context.Context, runID string) ([]interpreter.ReplayEntry, error) {
		return history, nil
	}
	appendHistory := func(ctx context.Context, runID, intentID string, entry interpreter.ReplayEntry) error {
		history = append(history, entry)
		return nil
	}

	it := interpreter.NewStarlark(h.Registry, zerolog.Nop()).WithHistory(load, appendHistory)
	intent := &orchestrator.Intent{
		ID:     "test_replay",
		Script: script,
	}

	// --- FIRST RUN ---
	ctx1, cancel1 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel1()

	errChan1 := make(chan error, 1)
	go func() {
		err := it.Execute(ctx1, intent, "main", interpreter.RunOptions{
			RunID:      "run_1",
			Entrypoint: "main",
		})
		errChan1 <- err
	}()

	// Wait for the first QA to appear
	foundFirst := false
	for i := 0; i < 20; i++ {
		if h.TUIModel.content.hasActiveQA() {
			foundFirst = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !foundFirst {
		t.Fatal("First QA item never appeared")
	}

	// Answer the first prompt
	h.SendKey("1")

	// Wait for the second QA to appear (this ensures the first one was processed and recorded)
	foundSecond := false
	for i := 0; i < 20; i++ {
		// Check if there's an active QA and it's the second one
		if h.TUIModel.content.hasActiveQA() {
			for _, item := range h.TUIModel.content.items {
				if item.Type == "qa" && item.Text == "Second Prompt" {
					foundSecond = true
					break
				}
			}
		}
		if foundSecond {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !foundSecond {
		t.Fatal("Second QA item never appeared in first run")
	}

	// Now cancel the first run
	cancel1()
	<-errChan1 // Wait for it to exit

	// Verify history has one entry
	if len(history) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(history))
	}

	// Record the number of items in TUI history so we can verify the skip
	initialItemCount := len(h.TUIModel.content.items)

	// --- SECOND RUN (REPLAY) ---
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()

	errChan2 := make(chan error, 1)
	go func() {
		// Use the SAME RunID so it triggers history loading if implemented that way, 
		// though our 'load' function ignores RunID and returns the captured 'history'.
		err := it.Execute(ctx2, intent, "main", interpreter.RunOptions{
			RunID:      "run_1",
			Entrypoint: "main",
		})
		errChan2 <- err
	}()

	// Wait for the second QA to appear
	foundSecondReplay := false
	for i := 0; i < 20; i++ {
		if h.TUIModel.content.hasActiveQA() {
			for _, item := range h.TUIModel.content.items {
				if item.Type == "qa" && item.Text == "Second Prompt" {
					foundSecondReplay = true
					break
				}
			}
		}
		if foundSecondReplay {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !foundSecondReplay {
		t.Fatal("Second QA item never appeared in replay")
	}

	// Verify that "First Prompt" was NOT added to TUI history again
	// The content model might have the items from the previous run still, 
	// so we check how many NEW items were added.
	// Actually, the TUI model is shared across runs in this test.
	// Let's count occurrences of "First Prompt" in QA items.
	firstPromptCount := 0
	for _, item := range h.TUIModel.content.items {
		// In ContentModel.answerQA, the item is changed to "notification" and text is updated.
		// So we should check for "First Prompt" in the text.
		if strings.Contains(item.Text, "First Prompt") {
			firstPromptCount++
		}
	}
	if firstPromptCount != 1 {
		t.Errorf("expected First Prompt to appear only once in TUI history, found %d", firstPromptCount)
	}

	// Answer the second prompt
	h.SendKey("1")

	// Wait for completion
	select {
	case err := <-errChan2:
		if err != nil {
			t.Fatalf("Replay execution failed: %v", err)
		}
	case <-ctx2.Done():
		t.Fatal("Replay timed out")
	}
}
```

- [ ] **Step 2: Run the test to verify it passes**

Run: `go test -v internal/tui/integration_test.go`
Expected: PASS

- [ ] **Step 3: Commit the changes**

```bash
git add internal/tui/integration_test.go
git commit -m "test: verify Starlark replay logic for interactive TUI prompts"
```

Plan complete and saved to `docs/superpowers/plans/2026-04-05-replay-validation-test.md`. Two execution options:

1. Subagent-Driven (recommended) - I dispatch a fresh subagent per task, review between tasks, fast iteration

2. Inline Execution - Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?
