package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/brightpuddle/clara/internal/interpreter"
	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"
)

func TestIntegration_Starlark_Interaction(t *testing.T) {
	h := NewE2EHarness(t)

	if err := h.StartTUI(); err != nil {
		t.Fatalf("Failed to start TUI: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Script that calls the interactive notification and returns the result.
	script := `
def main():
    res = tui.notify.send_interactive(
        prompt="Execute order 66?",
        options=["Yes, Lord Vader", "No, I'm a Jedi"]
    )
    return res
`

	var finalMem map[string]any
	it := interpreter.NewStarlark(h.Registry, zerolog.Nop()).WithOnChange(func(ctx context.Context, runID, intentID, stateName string, mem map[string]any) {
		finalMem = mem
	})
	intent := &orchestrator.Intent{
		ID:     "test_starlark_interaction",
		Script: script,
	}

	errChan := make(chan error, 1)
	go func() {
		err := it.Execute(ctx, intent, "main", interpreter.RunOptions{
			RunID:      "test_run",
			Entrypoint: "main",
		})
		errChan <- err
	}()

	h.verifyTUIHasActiveQA(t, "Execute order 66?")

	// Send answer '1' (Yes, Lord Vader)
	h.SendKey("1")

	// Wait for the script to complete
	select {
	case err := <-errChan:
		if err != nil {
			t.Fatalf("Starlark execution failed: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("Test timed out waiting for Starlark completion")
	}

	// Verify the script's return value
	if finalMem == nil {
		t.Fatal("onChange was never called")
	}
	result, ok := finalMem["main_result"].(string)
	if !ok {
		t.Fatalf("expected string result, got %T: %v", finalMem["main_result"], finalMem["main_result"])
	}

	expected := "Answer received: Yes, Lord Vader"
	if result != expected {
		t.Errorf("expected result %q, got %q", expected, result)
	}
}

func TestIntegration_SendInteractive_Blocks(t *testing.T) {
	h := NewE2EHarness(t)

	if err := h.StartTUI(); err != nil {
		t.Fatalf("Failed to start TUI: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	prompt := "Continue with operation?"
	options := []string{"Yes", "No"}

	resultChan := make(chan *mcp.CallToolResult, 1)
	errChan := make(chan error, 1)

	go func() {
		res, err := h.CallTool(ctx, "tui.notify.send_interactive", map[string]any{
			"prompt":  prompt,
			"options": options,
		})
		if err != nil {
			errChan <- err
			return
		}
		resultChan <- res
	}()

	h.verifyTUIHasActiveQA(t, prompt)

	// Ensure the tool call is still blocking
	select {
	case res := <-resultChan:
		t.Fatalf("tool call returned prematurely with result: %v", res)
	case err := <-errChan:
		t.Fatalf("tool call failed prematurely with error: %v", err)
	case <-time.After(500 * time.Millisecond):
		// This is the expected path: it should still be blocking
	}

	// Send answer '1' (Yes)
	h.SendKey("1")

	// Now it should unblock
	select {
	case res := <-resultChan:
		if res.IsError {
			t.Fatalf("tool call returned error result: %v", res.Content)
		}
		t.Logf("Got result: %v", res.Content)
	case err := <-errChan:
		t.Fatalf("tool call failed: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("tool call timed out waiting for answer")
	}
}

func TestIntegration_Send_NonBlocking(t *testing.T) {
	h := NewE2EHarness(t)

	if err := h.StartTUI(); err != nil {
		t.Fatalf("Failed to start TUI: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	message := "Hello, TUI!"

	res, err := h.CallTool(ctx, "tui.notify.send", map[string]any{
		"message": message,
	})
	if err != nil {
		t.Fatalf("failed to call tui.notify.send: %v", err)
	}

	if res.IsError {
		t.Fatalf("tool call returned error: %v", res.Content)
	}

	h.verifyTUIHasText(t, message)
}

func TestIntegration_Starlark_Replay(t *testing.T) {
	// Script that calls notify.send_interactive twice
	script := `
def main():
    res1 = tui.notify.send_interactive(
        prompt="First Prompt",
        options=["A", "B"]
    )
    res2 = tui.notify.send_interactive(
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

	intent := &orchestrator.Intent{
		ID:     "test_replay",
		Script: script,
	}

	// --- FIRST RUN ---
	h1 := NewE2EHarness(t)
	if err := h1.StartTUI(); err != nil {
		t.Fatalf("Failed to start TUI 1: %v", err)
	}
	it1 := interpreter.NewStarlark(h1.Registry, zerolog.Nop()).WithHistory(load, appendHistory)

	ctx1, cancel1 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel1()

	errChan1 := make(chan error, 1)
	go func() {
		err := it1.Execute(ctx1, intent, "main", interpreter.RunOptions{
			RunID:      "run_1",
			Entrypoint: "main",
		})
		errChan1 <- err
	}()

	h1.verifyTUIHasActiveQA(t, "First Prompt")

	// Answer the first prompt
	h1.SendKey("1")
	time.Sleep(1 * time.Second)

	// Wait for the second QA to appear
	h1.verifyTUIHasActiveQA(t, "Second Prompt")

	// Now cancel and close first harness
	cancel1()
	select {
	case <-errChan1:
		// Expected: context cancelled or completed with error — both are fine
	case <-time.After(3 * time.Second):
		t.Fatal("First run did not stop after context cancellation")
	}
	h1.StopTUI()
	time.Sleep(500 * time.Millisecond)

	// Truncate history to only the first successful entry
	if len(history) > 1 {
		history = history[:1]
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(history))
	}

	// --- SECOND RUN (REPLAY) ---
	h2 := NewE2EHarness(t)
	if err := h2.StartTUI(); err != nil {
		t.Fatalf("Failed to start TUI 2: %v", err)
	}
	it2 := interpreter.NewStarlark(h2.Registry, zerolog.Nop()).WithHistory(load, appendHistory)

	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()

	errChan2 := make(chan error, 1)
	go func() {
		err := it2.Execute(ctx2, intent, "main", interpreter.RunOptions{
			RunID:      "run_1",
			Entrypoint: "main",
		})
		errChan2 <- err
	}()

	h2.verifyTUIHasActiveQA(t, "Second Prompt")

	// Verify that "First Prompt" NEVER appeared in the second TUI
	for _, item := range h2.TUISnapshot().items {
		if strings.Contains(item.Text, "First Prompt") {
			t.Errorf("First Prompt should have been skipped in replay, but found it in TUI history")
		}
	}

	// Answer the second prompt
	h2.SendKey("1")

	// Wait for completion
	select {
	case err := <-errChan2:
		if err != nil {
			t.Fatalf("Replay execution failed: %v", err)
		}
	case <-ctx2.Done():
		t.Fatal("Replay timed out waiting for second prompt to be answered")
	}
}

func TestIntegration_SequentialQA(t *testing.T) {
	h := NewE2EHarness(t)

	if err := h.StartTUI(); err != nil {
		t.Fatalf("Failed to start TUI: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Send two interactive notifications in parallel
	resultChan1 := make(chan *mcp.CallToolResult, 1)
	resultChan2 := make(chan *mcp.CallToolResult, 1)

	go func() {
		res, _ := h.CallTool(ctx, "tui.notify.send_interactive", map[string]any{
			"prompt":  "Prompt 1",
			"options": []string{"A", "B"},
		})
		resultChan1 <- res
	}()

	go func() {
		// Small delay to ensure order in current implementation (though it might still be racey)
		time.Sleep(200 * time.Millisecond)
		h.CallTool(ctx, "tui.notify.send", map[string]any{
			"message": "Between 1 and 2",
		})
		res, _ := h.CallTool(ctx, "tui.notify.send_interactive", map[string]any{
			"prompt":  "Prompt 2",
			"options": []string{"C", "D"},
		})
		resultChan2 <- res
	}()

	h.verifyTUIHasActiveQA(t, "Prompt 1")

	// Verify ONLY Prompt 1 is visible
	qaCount := 0
	for _, item := range h.TUISnapshot().items {
		if item.Type == "qa" {
			qaCount++
		}
	}
	if qaCount > 1 {
		t.Errorf("expected only 1 active QA, got %d", qaCount)
	}

	// Answer Prompt 1
	time.Sleep(1 * time.Second)
	h.SendKey("1")

	// Now Prompt 2 AND the intermediate notification should appear
	h.verifyTUIHasActiveQA(t, "Prompt 2")
	h.verifyTUIHasText(t, "Between 1 and 2")

	// Answer Prompt 2
	h.SendKey("1")

	// Wait for both to complete
	select {
	case <-resultChan1:
	case <-ctx.Done():
		t.Fatal("Prompt 1 timed out")
	}
	select {
	case <-resultChan2:
	case <-ctx.Done():
		t.Fatal("Prompt 2 timed out")
	}
}
