package registry

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

const (
	// readOnlyOutputCap bounds stored output of list/get/read style tools, whose
	// results are large, repetitive and rarely useful in an audit trail.
	readOnlyOutputCap = 1024
	// defaultOutputCap bounds every other tool's stored output.
	defaultOutputCap = 16 * 1024
	// inputCap bounds stored tool arguments.
	inputCap = 16 * 1024
)

// auditPolicy says how much of a tool call is worth persisting.
type auditPolicy struct {
	skip      bool
	maxOutput int
}

// pollingTools are called every second or two by supervised workers; auditing
// them only produces millions of rows.
var pollingTools = map[string]bool{
	"agent.hub.read":       true,
	"agent.events.read":    true,
	"agent.attention.list": true,
}

var readOnlyMarkers = []string{".list", "_list", ".get", "_get", ".read", "_read", "search"}

func auditPolicyFor(tool string) auditPolicy {
	if pollingTools[tool] {
		return auditPolicy{skip: true}
	}
	for _, m := range readOnlyMarkers {
		if strings.Contains(tool, m) {
			return auditPolicy{maxOutput: readOnlyOutputCap}
		}
	}
	return auditPolicy{maxOutput: defaultOutputCap}
}

// capAuditJSON returns s unchanged when it fits in max bytes; otherwise a small
// valid JSON document holding the original size and a prefix of s.
func capAuditJSON(s string, max int) string {
	if len(s) <= max {
		return s
	}
	preview := s[:max]
	for len(preview) > 0 && !utf8.ValidString(preview) {
		preview = preview[:len(preview)-1]
	}
	// Leave room for the envelope; JSON escaping can only grow the preview.
	for {
		b, _ := json.Marshal(map[string]any{
			"truncated": true, "bytes": len(s), "preview": preview,
		})
		if len(b) <= max+64 || len(preview) == 0 {
			return string(b)
		}
		preview = preview[:len(preview)/2]
		for len(preview) > 0 && !utf8.ValidString(preview) {
			preview = preview[:len(preview)-1]
		}
	}
}
