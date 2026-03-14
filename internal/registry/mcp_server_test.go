package registry

import "testing"

func TestPreferredServiceDescription(t *testing.T) {
	if got := preferredServiceDescription("Configured description", "Discovered description"); got != "Configured description" {
		t.Fatalf("configured description should win, got %q", got)
	}

	if got := preferredServiceDescription("", "Discovered description"); got != "Discovered description" {
		t.Fatalf("discovered description should be used as fallback, got %q", got)
	}
}
