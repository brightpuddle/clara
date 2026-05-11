package discordapi

import (
	"testing"
	"time"
)

func TestRouterInteractiveDecision(t *testing.T) {
	r := NewRouter()
	requestID := "test-req-1"

	ch := r.RegisterInteractive(requestID)

	go func() {
		time.Sleep(10 * time.Millisecond)
		r.ResolveInteractive(requestID, InteractiveDecision{
			Selection:  "custom",
			CustomText: "my feedback",
		})
	}()

	decision, ok := r.WaitInteractive(requestID, ch, 1*time.Second)
	if !ok {
		t.Fatal("expected decision, got timeout")
	}
	if decision.Selection != "custom" || decision.CustomText != "my feedback" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}
