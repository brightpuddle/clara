package cockpit

import (
	"testing"
	"time"

	"github.com/brightpuddle/clara/internal/agents"
)

func TestAttentionNotifierShouldNotify(t *testing.T) {
	notifier := NewAttentionNotifier(AttentionConfig{
		Enabled:          true,
		NotifyStates:     []string{"blocked", "needs_you", "done", "failed"},
		SuppressAttached: true,
	})

	// 1. Busy -> Needs attention (attention.requested)
	ar := agents.AttentionRequested{
		AttentionID: "att-1",
		Kind:        agents.AttentionTurnEnded,
		Title:       "Finished refactor of session.go",
	}
	should, title, body := notifier.EvaluateAttention(ar, "01M6TESTSESSION", "eve", "clara")
	if !should {
		t.Fatalf("expected attention.requested to notify")
	}
	if title == "" || body == "" {
		t.Fatalf("expected non-empty title and body, got %q, %q", title, body)
	}

	// 2. Blocked status (quota / error)
	statusBlocked := agents.SessionStatus{
		State:  agents.StateBlocked,
		Reason: "quota",
	}
	shouldB, titleB, _ := notifier.EvaluateStatus(statusBlocked, "01M6TESTSESSION", "eve", "clara")
	if !shouldB {
		t.Fatalf("expected blocked status to notify")
	}
	if titleB == "" {
		t.Fatalf("expected non-empty title")
	}

	// 3. Normal running status should NOT notify
	statusRunning := agents.SessionStatus{
		State: agents.StateRunning,
	}
	shouldR, _, _ := notifier.EvaluateStatus(statusRunning, "01M6TESTSESSION", "eve", "clara")
	if shouldR {
		t.Fatalf("did not expect running state to notify")
	}

	// 4. Suppression for attached session
	notifier.SetAttachedSession("01M6TESTSESSION")
	shouldAtt, _, _ := notifier.EvaluateAttention(ar, "01M6TESTSESSION", "eve", "clara")
	if shouldAtt {
		t.Fatalf("expected currently attached session to be suppressed from notifications")
	}

	// 5. Quiet hours test
	now := time.Now()
	notifier.SetAttachedSession("") // clear attached
	notifier.cfg.QuietHoursStart = (now.Hour() + 23) % 24
	notifier.cfg.QuietHoursEnd = (now.Hour() + 1) % 24
	shouldQuiet, _, _ := notifier.EvaluateAttention(ar, "01M6TESTSESSION", "eve", "clara")
	if shouldQuiet {
		t.Fatalf("expected quiet hours to suppress notification")
	}
}
