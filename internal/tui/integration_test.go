package tui

import (
	"context"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

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
