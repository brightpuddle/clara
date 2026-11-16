package adapter

import (
	"context"

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

// OpencodeQuirks: opencode selects models through a config option.
var OpencodeQuirks = Quirks{
	SetModel: func(ctx context.Context, c RPC, sessionID, model string) error {
		return c.Call(ctx, "session/set_config_option",
			map[string]any{"sessionId": sessionID, "configId": "model", "value": model}, nil)
	},
}
