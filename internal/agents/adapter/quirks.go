package adapter

import (
	"context"
	"regexp"
	"strconv"
	"time"

	"github.com/brightpuddle/clara/internal/agents"
)

// Quirks holds the per-agent differences the ACP adapter cannot infer.
type Quirks struct {
	// SetModel selects a model on a fresh session. Nil means unsupported.
	SetModel func(ctx context.Context, c RPC, sessionID, model string) error
	// DetectBlocked inspects a finished assistant message for agent-reported
	// limits that arrive as ordinary text. Nil means never blocked.
	DetectBlocked func(text string) *agents.Blocked
}

// AgyQuirks: agy has session/set_model and reports quota exhaustion as a normal
// message followed by end_turn.
var AgyQuirks = Quirks{
	SetModel: func(ctx context.Context, c RPC, sessionID, model string) error {
		return c.Call(ctx, "session/set_model",
			map[string]any{"sessionId": sessionID, "modelId": model}, nil)
	},
	DetectBlocked: detectQuota,
}

// OpencodeQuirks: opencode selects models through a config option.
var OpencodeQuirks = Quirks{
	SetModel: func(ctx context.Context, c RPC, sessionID, model string) error {
		return c.Call(ctx, "session/set_config_option",
			map[string]any{"sessionId": sessionID, "configId": "model", "value": model}, nil)
	},
}

var (
	quotaRe  = regexp.MustCompile(`(?i)\bquota\b.{0,40}\b(reached|exceeded|exhausted)\b`)
	resetsRe = regexp.MustCompile(`(?i)resets? in\s+(?:(\d+)\s*h)?\s*(?:(\d+)\s*m)?`)
)

func detectQuota(text string) *agents.Blocked {
	if !quotaRe.MatchString(text) {
		return nil
	}
	b := &agents.Blocked{Kind: "quota"}
	if m := resetsRe.FindStringSubmatch(text); m != nil && (m[1] != "" || m[2] != "") {
		h, _ := strconv.Atoi(m[1])
		min, _ := strconv.Atoi(m[2])
		at := time.Now().Add(time.Duration(h)*time.Hour + time.Duration(min)*time.Minute)
		b.ResetsAt = &at
	}
	return b
}
