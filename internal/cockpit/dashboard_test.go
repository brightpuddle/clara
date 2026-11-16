package cockpit

import (
	"testing"
)

func TestSortSessionsNeedsYouFirst(t *testing.T) {
	sessions := []SessionSummary{
		{ID: "s1", State: "idle"},
		{ID: "s2", State: "blocked"},
		{ID: "s3", State: "running"},
		{ID: "s4", State: "closed"},
		{ID: "s5", State: "failed"},
	}

	sorted := SortSessions(sessions)

	// Expected order priority: blocked / failed / needs_you -> running -> idle -> closed
	if sorted[0].State != "blocked" && sorted[0].State != "failed" {
		t.Fatalf("expected blocked or failed first, got %s", sorted[0].State)
	}
	if sorted[len(sorted)-1].State != "closed" {
		t.Fatalf("expected closed last, got %s", sorted[len(sorted)-1].State)
	}
}

func TestFilterSessions(t *testing.T) {
	sessions := []SessionSummary{
		{ID: "01M6TEST1", Title: "Fix router bug", Dir: "/Users/nathan/src/clara", Agent: "opencode", State: "idle"},
		{ID: "01M6TEST2", Title: "Lucia typechecker", Dir: "/Users/nathan/src/lucia", Agent: "opencode", State: "idle"},
		{ID: "01M6TEST3", Title: "Mail triage", Dir: "/Users/nathan/.config/clara", Agent: "opencode", State: "closed"},
	}

	// Filter with includeClosed = false (default in cockpit top)
	matches := FilterSessions(sessions, "lucia", false)
	if len(matches) != 1 || matches[0].ID != "01M6TEST2" {
		t.Fatalf("expected 1 match for lucia, got %d", len(matches))
	}

	matchesClara := FilterSessions(sessions, "clara", false)
	if len(matchesClara) != 1 {
		t.Fatalf("expected 1 match for active clara, got %d", len(matchesClara))
	}

	matchesClaraAll := FilterSessions(sessions, "clara", true)
	if len(matchesClaraAll) != 2 {
		t.Fatalf("expected 2 matches for clara including closed, got %d", len(matchesClaraAll))
	}

	// Empty query with closed hidden vs all
	activeOnly := FilterSessions(sessions, "", false)
	if len(activeOnly) != 2 {
		t.Fatalf("expected 2 active sessions, got %d", len(activeOnly))
	}
}
