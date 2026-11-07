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
		{ID: "01M6TEST1", Title: "Fix router bug", Dir: "/Users/nathan/src/clara", Agent: "agy"},
		{ID: "01M6TEST2", Title: "Lucia typechecker", Dir: "/Users/nathan/src/lucia", Agent: "opencode"},
		{ID: "01M6TEST3", Title: "Mail triage", Dir: "/Users/nathan/.config/clara", Agent: "agy"},
	}

	matches := FilterSessions(sessions, "lucia")
	if len(matches) != 1 || matches[0].ID != "01M6TEST2" {
		t.Fatalf("expected 1 match for lucia, got %d", len(matches))
	}

	matchesAgy := FilterSessions(sessions, "agy")
	if len(matchesAgy) != 2 {
		t.Fatalf("expected 2 matches for agy, got %d", len(matchesAgy))
	}
}
