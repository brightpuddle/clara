package cockpit

import (
	"fmt"
	"strings"
)

// FormatStatusShort formats a short summary for tmux/WezTerm status bars: e.g. "2 running, 1 blocked".
func FormatStatusShort(sessions []SessionSummary) string {
	counts := map[string]int{
		"running": 0,
		"blocked": 0,
		"idle":    0,
	}

	for _, s := range sessions {
		if s.State == "closed" {
			continue
		}
		if _, ok := counts[s.State]; ok {
			counts[s.State]++
		} else if s.State != "" {
			counts[s.State]++
		}
	}

	var parts []string
	if counts["running"] > 0 {
		parts = append(parts, fmt.Sprintf("%d running", counts["running"]))
	}
	if counts["blocked"] > 0 {
		parts = append(parts, fmt.Sprintf("%d blocked", counts["blocked"]))
	}
	if counts["idle"] > 0 {
		parts = append(parts, fmt.Sprintf("%d idle", counts["idle"]))
	}

	if len(parts) == 0 {
		return "idle"
	}
	return strings.Join(parts, ", ")
}
