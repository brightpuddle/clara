package cockpit

import (
	"sort"
	"strings"
)

// statePriority defines the ordering for dashboard sorting: needs attention first.
func statePriority(state string) int {
	switch strings.ToLower(state) {
	case "blocked", "failed", "needs_you":
		return 0
	case "running":
		return 1
	case "idle":
		return 2
	case "closed":
		return 3
	default:
		return 2
	}
}

// SortSessions sorts sessions with blocked/needs_you first, then running, idle, and closed last.
func SortSessions(sessions []SessionSummary) []SessionSummary {
	out := make([]SessionSummary, len(sessions))
	copy(out, sessions)

	sort.SliceStable(out, func(i, j int) bool {
		pi := statePriority(out[i].State)
		pj := statePriority(out[j].State)
		if pi != pj {
			return pi < pj
		}
		// Sub-sort by title or ID
		if out[i].Title != out[j].Title {
			return out[i].Title < out[j].Title
		}
		return out[i].ID > out[j].ID
	})

	return out
}

// FilterSessions filters sessions by matching query against title, ID, agent, host, or directory.
// When includeClosed is false, sessions with State == "closed" are excluded.
func FilterSessions(sessions []SessionSummary, query string, includeClosed bool) []SessionSummary {
	q := strings.TrimSpace(strings.ToLower(query))

	var matched []SessionSummary
	for _, s := range sessions {
		if !includeClosed && strings.ToLower(s.State) == "closed" {
			continue
		}
		if q == "" {
			matched = append(matched, s)
			continue
		}
		if strings.Contains(strings.ToLower(s.Title), q) ||
			strings.Contains(strings.ToLower(s.ID), q) ||
			strings.Contains(strings.ToLower(s.Agent), q) ||
			strings.Contains(strings.ToLower(s.Host), q) ||
			strings.Contains(strings.ToLower(s.Dir), q) {
			matched = append(matched, s)
		}
	}
	return matched
}
