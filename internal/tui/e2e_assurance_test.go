package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/brightpuddle/clara/internal/store"
)

func TestAssurance_TUIOpen_Send(t *testing.T) {
	h := NewE2EHarness(t)

	if err := h.StartTUI(); err != nil {
		t.Fatalf("Failed to start TUI: %v", err)
	}

	t.Run("tui.notify.send tool", func(t *testing.T) {
		message := "Notification 1"
		_, err := h.Registry.Call(
			context.Background(),
			"tui.notify.send",
			map[string]any{"message": message},
		)
		if err != nil {
			t.Fatalf("Tool call failed: %v", err)
		}
		h.verifyTUIHasText(t, message)
	})

	t.Run("clara tool call tui.notify.send", func(t *testing.T) {
		message := "Notification 2"
		resp, err := h.CLIToolCall("tui.notify.send", map[string]any{"message": message})
		if err != nil {
			t.Fatalf("CLI call failed: %v", err)
		}
		if resp.Error != "" {
			t.Fatalf("CLI call returned error: %s", resp.Error)
		}
		h.verifyTUIHasText(t, message)
	})
}

func TestAssurance_TUIOpen_SendInteractive(t *testing.T) {
	h := NewE2EHarness(t)

	if err := h.StartTUI(); err != nil {
		t.Fatalf("Failed to start TUI: %v", err)
	}

	t.Run("tui.notify.send_interactive tool blocks and resumes", func(t *testing.T) {
		prompt := "Continue?"
		done := make(chan any, 1)
		go func() {
			res, err := h.Registry.Call(
				context.Background(),
				"tui.notify.send_interactive",
				map[string]any{
					"prompt":  prompt,
					"options": []string{"Yes", "No"},
				},
			)
			if err != nil {
				t.Logf("Tool call failed: %v", err)
				done <- err
				return
			}
			done <- res
		}()

		h.verifyTUIHasActiveQA(t, prompt)
		h.SendKey("1")

		select {
		case res := <-done:
			if err, ok := res.(error); ok {
				t.Fatalf("Tool call failed: %v", err)
			}
			if res != "Answer received: Yes" {
				t.Errorf("Expected answer Yes, got %v", res)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Tool call timed out")
		}
	})

	t.Run(
		"clara tool call tui.notify.send_interactive blocks and logs response",
		func(t *testing.T) {
			prompt := "CLI Prompt?"
			done := make(chan any, 1)
			go func() {
				resp, err := h.CLIToolCall("tui.notify.send_interactive", map[string]any{
					"prompt":  prompt,
					"options": []string{"A", "B"},
				})
				if err != nil {
					t.Logf("CLI call failed: %v", err)
					done <- err
					return
				}
				done <- resp.Data
			}()

			h.verifyTUIHasActiveQA(t, prompt)
			h.SendKey("1")

			select {
			case res := <-done:
				if err, ok := res.(error); ok {
					t.Fatalf("CLI call failed: %v", err)
				}
				if res != "Answer received: A" {
					t.Errorf("Expected answer A, got %v", res)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("CLI call timed out")
			}
		},
	)
}

func TestAssurance_TUIOffline_Send(t *testing.T) {
	h := NewE2EHarness(t)

	t.Run("tui.notify.send tool logs in intent logs and continues", func(t *testing.T) {
		message := "Offline message"
		intentID := "intent-1"
		ctx := context.WithValue(context.Background(), orchestrator.ContextKeyIntentID, intentID)

		res, err := h.Registry.Call(ctx, "tui.notify.send", map[string]any{"message": message})
		if err != nil {
			t.Fatalf("Tool call failed: %v", err)
		}
		if res != "notification recorded (TUI offline)" {
			t.Errorf("Unexpected response: %v", res)
		}

		// Verify it's in DB
		history, _ := h.Store.LoadTUIContentHistory(context.Background(), 1)
		if len(history) == 0 || history[0].Text != message {
			t.Errorf("Message not found in DB history")
		}
	})

	t.Run("clara tool call tui.notify.send logs and terminates", func(t *testing.T) {
		message := "CLI Offline message"
		resp, err := h.CLIToolCall("tui.notify.send", map[string]any{"message": message})
		if err != nil {
			t.Fatalf("CLI call failed: %v", err)
		}
		if resp.Error != "" {
			t.Fatalf("CLI call returned error: %s", resp.Error)
		}
	})
}

func TestAssurance_TUIOffline_SendInteractive(t *testing.T) {
	// Use separate harnesses to avoid sequence pollution
	t.Run("tui.notify.send_interactive in starlark queues and exits", func(t *testing.T) {
		h := NewE2EHarness(t)

		prompt := "Queue me"
		intentID := "intent-2"
		runID := "run-2"
		ctx := context.WithValue(context.Background(), orchestrator.ContextKeyIntentID, intentID)
		ctx = context.WithValue(ctx, orchestrator.ContextKeyRunID, runID)

		_, err := h.Registry.Call(ctx, "tui.notify.send_interactive", map[string]any{
			"prompt":  prompt,
			"options": []string{"Yes", "No"},
		})
		if err == nil || !strings.Contains(err.Error(), "workflow paused") {
			t.Fatalf("Expected workflow paused error, got %v", err)
		}

		// Verify it's in DB as unanswered
		id, _ := h.Store.GetUnansweredTUIPrompt(context.Background(), intentID, prompt)
		if id == 0 {
			t.Fatal("Prompt not found in DB")
		}
	})

	t.Run(
		"clara tool call tui.notify.send_interactive blocks until TUI answers",
		func(t *testing.T) {
			h := NewE2EHarness(t)

			prompt := "CLI Offline Prompt"

			if err := h.StartTUI(); err != nil {
				t.Fatalf("Failed to start TUI: %v", err)
			}

			done := make(chan any, 1)
			go func() {
				resp, err := h.CLIToolCall("tui.notify.send_interactive", map[string]any{
					"prompt":  prompt,
					"options": []string{"X", "Y"},
				})
				if err != nil {
					t.Logf("CLI call failed: %v", err)
					done <- err
					return
				}
				done <- resp.Data
			}()

			h.verifyTUIHasActiveQA(t, prompt)
			h.SendKey("1")
			select {
			case res := <-done:
				if err, ok := res.(error); ok {
					t.Fatalf("CLI call failed: %v", err)
				}
				if res != "Answer received: X" {
					t.Errorf("Expected answer X, got %v", res)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("CLI call timed out")
			}
		},
	)
}

func TestAssurance_CLIToolCall_Interactive_Breakout(t *testing.T) {
	h := NewE2EHarness(t)

	prompt := "Breakout Prompt"
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		// We simulate the blocking by calling it directly via registry with a cancellable context
		_, err := h.Registry.Call(ctx, "tui.notify.send_interactive", map[string]any{
			"prompt":  prompt,
			"options": []string{"A", "B"},
		})
		done <- err
	}()

	time.Sleep(200 * time.Millisecond)

	// Verify it's in DB
	id, _ := h.Store.GetUnansweredTUIPrompt(context.Background(), "", prompt)
	if id == 0 {
		t.Fatal("Prompt should be in DB while blocking")
	}

	// Break out (cancel CLI)
	cancel()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "context canceled") {
			t.Errorf("Expected context canceled error, got %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Breakout timed out")
	}

	// Verify it's REMOVED from DB
	idAfter, _ := h.Store.GetUnansweredTUIPrompt(context.Background(), "", prompt)
	if idAfter != 0 {
		t.Error("Prompt should have been removed from DB after breakout")
	}
}

func TestAssurance_TUI_Persistence_And_Sequence(t *testing.T) {
	h := NewE2EHarness(t)

	t.Log("1. Queueing two interactive notifications")
	_, _ = h.Store.SaveTUIContent(context.Background(), store.TUIContentItem{
		Kind: "qa", Text: "Prompt 1", Options: []string{"A", "B"},
	})
	time.Sleep(10 * time.Millisecond)
	_, _ = h.Store.SaveTUIContent(context.Background(), store.TUIContentItem{
		Kind: "qa", Text: "Prompt 2", Options: []string{"C", "D"},
	})

	t.Log("2. Opening TUI")
	if err := h.StartTUI(); err != nil {
		t.Fatalf("Failed to start TUI: %v", err)
	}

	t.Log("3. Verifying Prompt 1 is presented first")
	h.verifyTUIHasActiveQA(t, "Prompt 1")

	t.Log("4. Answering Prompt 1")
	h.SendKey("1")
	time.Sleep(500 * time.Millisecond)

	t.Log("5. Verifying Prompt 2 is presented next")
	h.verifyTUIHasActiveQA(t, "Prompt 2")

	t.Log("6. Answering Prompt 2")
	h.SendKey("1")
	time.Sleep(500 * time.Millisecond)

	t.Log("7. Verifying NO active prompts")
	if h.TUISnapshot().hasActiveQA() {
		t.Error("TUI should not have active QA after answering both")
	}

	t.Log("8. Closing and reopening TUI")
	h.StopTUI()
	if err := h.StartTUIWithHistory(2); err != nil {
		t.Fatalf("Failed to restart TUI: %v", err)
	}

	t.Log("9. Verifying NO active prompts after reopening")
	snap := h.TUISnapshot()
	if snap.hasActiveQA() {
		t.Errorf(
			"REPRODUCED: TUI should NOT have active QA after restart if all were answered. Items: %v",
			snap.items,
		)
	}

	// Verify they are shown in history as answered
	found1, found2 := false, false
	for _, item := range snap.items {
		if strings.Contains(item.Text, "Prompt 1") && strings.Contains(item.Text, "Answered:") {
			found1 = true
		}
		if strings.Contains(item.Text, "Prompt 2") && strings.Contains(item.Text, "Answered:") {
			found2 = true
		}
	}
	if !found1 || !found2 {
		t.Errorf("Prompt history missing or not marked as answered. Items: %v", snap.items)
	}
}

func (h *E2EHarness) verifyTUIHasText(t *testing.T, text string) {
	t.Helper()
	found := false
	for range 20 {
		snap := h.TUISnapshot()
		for _, item := range snap.items {
			if strings.Contains(item.Text, text) {
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
		t.Errorf("Text %q never appeared in TUI. Items: %v", text, snap.items)
	}
}

func (h *E2EHarness) verifyTUIHasActiveQA(t *testing.T, text string) {
	t.Helper()
	found := false
	for range 20 {
		snap := h.TUISnapshot()
		if snap.hasActiveQA() {
			for _, item := range snap.items {
				if item.Type == "qa" && strings.Contains(item.Text, text) {
					found = true
					break
				}
			}
		}
		if found {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !found {
		snap := h.TUISnapshot()
		t.Errorf("Active QA %q never appeared in TUI. Items: %v", text, snap.items)
	}
}
