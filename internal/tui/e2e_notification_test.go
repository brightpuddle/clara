package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/brightpuddle/clara/internal/store"
)

func TestE2E_TUIOpen_Send(t *testing.T) {
	h := NewE2EHarness(t)

	if err := h.StartTUI(); err != nil {
		t.Fatalf("Failed to start TUI: %v", err)
	}

	message := "Hello from CLI"
	resp, err := h.CLIToolCall("tui.notify.send", map[string]any{"message": message})
	if err != nil {
		t.Fatalf("CLI call failed: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("CLI call returned error: %s", resp.Error)
	}

	found := false
	for i := 0; i < 20; i++ {
		snap := h.TUISnapshot()
		for _, item := range snap.items {
			if item.Text == message {
				found = true
				break
			}
		}
		if found {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !found {
		snap := h.TUISnapshot()
		t.Errorf("Message %q never appeared in TUI. Items: %v", message, snap.items)
	}
}

func TestE2E_Persistence_AnsweredNotRepeated(t *testing.T) {
	h := NewE2EHarness(t)

	intentID := "test-intent"
	prompt := "Continue?"
	
	_, err := h.Store.SaveTUIContent(context.Background(), store.TUIContentItem{
		IntentID: intentID,
		Kind:     "qa",
		Text:     prompt,
		Answer:   "Yes",
	})
	if err != nil {
		t.Fatalf("Failed to save answered QA: %v", err)
	}

	if err := h.StartTUI(); err != nil {
		t.Fatalf("Failed to start TUI: %v", err)
	}

	snap := h.TUISnapshot()
	if snap.hasActiveQA() {
		t.Error("TUI should not have active QA for already answered prompt")
	}

	foundAnswered := false
	for _, item := range snap.items {
		if strings.Contains(item.Text, prompt) && strings.Contains(item.Text, "Answered: Yes") {
			foundAnswered = true
			break
		}
	}
	if !foundAnswered {
		t.Errorf("TUI should show the answered prompt in history. Items: %v", snap.items)
	}

	ctx := context.WithValue(context.Background(), orchestrator.ContextKeyIntentID, intentID)
	ctx = context.WithValue(ctx, orchestrator.ContextKeyRunID, "run-1")
	res, err := h.Registry.Call(ctx, "tui.notify.send_interactive", map[string]any{
		"prompt":  prompt,
		"options": []string{"Yes", "No"},
	})
	if err != nil {
		t.Fatalf("Tool call failed: %v", err)
	}

	expected := "Answer received: Yes"
	if res != expected {
		t.Errorf("Expected auto-answer %q, got %v", expected, res)
	}

	if h.TUISnapshot().hasActiveQA() {
		t.Error("TUI should still not have active QA after re-calling send_interactive")
	}
}

func TestE2E_TUIOffline_SendInteractive_Queues(t *testing.T) {
	h := NewE2EHarness(t)

	prompt := "Ready when you are"
	
	ctx := context.WithValue(context.Background(), orchestrator.ContextKeyRunID, "run-1")
	_, err := h.Registry.Call(ctx, "tui.notify.send_interactive", map[string]any{
		"prompt":  prompt,
		"options": []string{"Yes", "No"},
	})
	if err == nil || !strings.Contains(err.Error(), "workflow paused") {
		t.Fatalf("Expected workflow paused error, got %v", err)
	}

	history, _ := h.Store.LoadTUIContentHistory(context.Background(), 10)
	found := false
	for _, item := range history {
		if item.Text == prompt && item.Kind == "qa" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Interactive prompt was not queued in DB")
	}

	if err := h.StartTUI(); err != nil {
		t.Fatalf("Failed to start TUI: %v", err)
	}

	snap := h.TUISnapshot()
	if !snap.hasActiveQA() {
		t.Error("TUI should have active QA from queued history")
	}

	foundInTUI := false
	for _, item := range snap.items {
		if item.Text == prompt && item.Type == "qa" {
			foundInTUI = true
			break
		}
	}
	if !foundInTUI {
		t.Errorf("Prompt %q not found as active QA in TUI. Items: %v", prompt, snap.items)
	}
}

func TestE2E_Persistence_OldestFirst(t *testing.T) {
	h := NewE2EHarness(t)

	h.Store.SaveTUIContent(context.Background(), store.TUIContentItem{Kind: "notification", Text: "First"})
	time.Sleep(10 * time.Millisecond)
	h.Store.SaveTUIContent(context.Background(), store.TUIContentItem{Kind: "notification", Text: "Second"})
	time.Sleep(10 * time.Millisecond)
	h.Store.SaveTUIContent(context.Background(), store.TUIContentItem{Kind: "notification", Text: "Third"})

	if err := h.StartTUI(); err != nil {
		t.Fatalf("Failed to start TUI: %v", err)
	}

	snap := h.TUISnapshot()
	var texts []string
	for _, item := range snap.items {
		if item.Text != "System online. Waiting for intents..." {
			texts = append(texts, item.Text)
		}
	}

	if len(texts) < 3 {
		t.Fatalf("Expected at least 3 items, got %d: %v", len(texts), texts)
	}

	if texts[0] != "First" || texts[1] != "Second" || texts[2] != "Third" {
		t.Errorf("Expected [First, Second, Third], got %v", texts)
	}
}

func TestE2E_SequentialPrompts_RestartTUI(t *testing.T) {
	h := NewE2EHarness(t)

	intentID := "seq-intent"
	ctx := context.WithValue(context.Background(), orchestrator.ContextKeyIntentID, intentID)
	ctx = context.WithValue(ctx, orchestrator.ContextKeyRunID, "run-seq")

	t.Log("1. Sending first prompt (offline)")
	prompt1 := "Step 1?"
	_, err := h.Registry.Call(ctx, "tui.notify.send_interactive", map[string]any{
		"prompt":  prompt1,
		"options": []string{"Yes", "No"},
	})
	if err == nil || !strings.Contains(err.Error(), "workflow paused") {
		t.Fatalf("Expected workflow paused for prompt 1, got %v", err)
	}

	t.Log("2. Starting TUI")
	if err := h.StartTUI(); err != nil {
		t.Fatalf("Failed to start TUI: %v", err)
	}
	if !h.TUISnapshot().hasActiveQA() {
		t.Fatal("TUI should have active QA for prompt 1")
	}

	t.Log("3. Answering prompt 1")
	h.SendKey("1")
	time.Sleep(500 * time.Millisecond)

	if h.TUISnapshot().hasActiveQA() {
		t.Fatal("TUI should NOT have active QA after answering prompt 1")
	}

	t.Log("4. Sending second prompt (online)")
	prompt2 := "Step 2?"
	done2 := make(chan bool)
	go func() {
		res, err := h.Registry.Call(ctx, "tui.notify.send_interactive", map[string]any{
			"prompt":  prompt2,
			"options": []string{"Yes", "No"},
		})
		if err != nil {
			t.Errorf("Tool call 2 failed: %v", err)
		}
		if res == nil || !strings.Contains(res.(string), "Answer received") {
			t.Errorf("Expected answer received for prompt 2, got %v", res)
		}
		close(done2)
	}()

	t.Log("5. Waiting for prompt 2 to appear in TUI")
	found2 := false
	for i := 0; i < 20; i++ {
		snap := h.TUISnapshot()
		for _, item := range snap.items {
			if item.Text == prompt2 && item.Type == "qa" {
				found2 = true
				break
			}
		}
		if found2 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !found2 {
		t.Fatal("Prompt 2 never appeared")
	}

	t.Log("6. Answering prompt 2")
	h.SendKey("1")
	
	select {
	case <-done2:
		t.Log("Prompt 2 tool call unblocked")
	case <-time.After(5 * time.Second):
		t.Fatal("Prompt 2 tool call timed out")
	}

	t.Log("7. Restarting TUI")
	h.StopTUI()
	time.Sleep(1 * time.Second)

	if err := h.StartTUI(); err != nil {

		t.Fatalf("Failed to start TUI: %v", err)
	}

	t.Log("8. Verifying NO active prompts")
	if h.TUISnapshot().hasActiveQA() {
		snap := h.TUISnapshot()
		t.Errorf("TUI should NOT have active QA after restart if all were answered. Items: %v", snap.items)
	}

	t.Log("9. Re-calling tools (replay simulation)")
	res1, _ := h.Registry.Call(ctx, "tui.notify.send_interactive", map[string]any{
		"prompt":  prompt1,
		"options": []string{"Yes", "No"},
	})
	if res1 == nil || !strings.Contains(res1.(string), "Answer received") {
		t.Errorf("Prompt 1 should have been auto-answered, got %v", res1)
	}

	res2, _ := h.Registry.Call(ctx, "tui.notify.send_interactive", map[string]any{
		"prompt":  prompt2,
		"options": []string{"Yes", "No"},
	})
	if res2 == nil || !strings.Contains(res2.(string), "Answer received") {
		t.Errorf("Prompt 2 should have been auto-answered, got %v", res2)
	}

	if h.TUISnapshot().hasActiveQA() {
		t.Error("TUI should still NOT have active QA after tool re-calls")
	}
}

func TestE2E_CLIToolCall_Interactive(t *testing.T) {
	h := NewE2EHarness(t)

	prompt := "CLI prompt?"
	options := []string{"A", "B"}

	if err := h.StartTUI(); err != nil {
		t.Fatalf("Failed to start TUI: %v", err)
	}

	done := make(chan bool)
	go func() {
		resp, err := h.CLIToolCall("tui.notify.send_interactive", map[string]any{
			"prompt":    prompt,
			"options":   options,
			"intent_id": "cli-intent",
		})
		if err != nil {
			t.Errorf("CLI call failed: %v", err)
		}
		if resp == nil || resp.Data == nil || !strings.Contains(resp.Data.(string), "Answer received: A") {
			t.Errorf("Expected answer A, got %v", resp)
		}
		close(done)
	}()

	found := false
	for i := 0; i < 20; i++ {
		if h.TUISnapshot().hasActiveQA() {
			found = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !found {
		t.Fatal("QA not found in TUI")
	}

	h.SendKey("1")
	time.Sleep(500 * time.Millisecond)

	select {
	case <-done:
		t.Log("CLI tool call unblocked")
	case <-time.After(10 * time.Second):
		t.Fatal("CLI tool call timed out")
	}
}
