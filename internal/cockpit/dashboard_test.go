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
		{ID: "01M6TEST1", Title: "Fix router bug", Dir: "/Users/nathan/src/clara", Agent: "agy", State: "idle"},
		{ID: "01M6TEST2", Title: "Lucia typechecker", Dir: "/Users/nathan/src/lucia", Agent: "opencode", State: "idle"},
		{ID: "01M6TEST3", Title: "Mail triage", Dir: "/Users/nathan/.config/clara", Agent: "agy", State: "closed"},
	}

	// Filter with includeClosed = false (default in cockpit top)
	matches := FilterSessions(sessions, "lucia", false)
	if len(matches) != 1 || matches[0].ID != "01M6TEST2" {
		t.Fatalf("expected 1 match for lucia, got %d", len(matches))
	}

	matchesAgy := FilterSessions(sessions, "agy", false)
	if len(matchesAgy) != 1 {
		t.Fatalf("expected 1 match for active agy, got %d", len(matchesAgy))
	}

	matchesAgyAll := FilterSessions(sessions, "agy", true)
	if len(matchesAgyAll) != 2 {
		t.Fatalf("expected 2 matches for agy including closed, got %d", len(matchesAgyAll))
	}

	// Empty query with closed hidden vs all
	activeOnly := FilterSessions(sessions, "", false)
	if len(activeOnly) != 2 {
		t.Fatalf("expected 2 active sessions, got %d", len(activeOnly))
	}
}
