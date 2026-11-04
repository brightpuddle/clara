package hub

import (
	"context"
	"encoding/json"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/internal/agents"
)

// CallFunc invokes a tool by name (registry.Registry.Call has this shape).
type CallFunc func(ctx context.Context, name string, args map[string]any) (any, error)

// ToolSource reads a host's log through its agent.events.read tool, optionally
// under a namespace prefix such as "laptop." for a mounted remote.
type ToolSource struct {
	call   CallFunc
	prefix string
}

// NewToolSource returns a Source backed by tool calls.
func NewToolSource(call CallFunc, prefix string) *ToolSource {
	return &ToolSource{call: call, prefix: prefix}
}

// ReadEvents implements Source.
func (s *ToolSource) ReadEvents(
	ctx context.Context, after int64, limit int,
) ([]agents.Event, error) {
	res, err := s.call(ctx, s.prefix+"agent.events.read",
		map[string]any{"after": after, "limit": limit})
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(res)
	if err != nil {
		return nil, errors.Wrap(err, "encode events.read result")
	}
	var out struct {
		Events []agents.Event `json:"events"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.Wrap(err, "decode events.read result")
	}
	return out.Events, nil
}

// Run pulls every interval until ctx is cancelled. Errors (for example the
// tunnel being down) are logged once per outage and retried on the next tick.
func (p *Puller) Run(ctx context.Context, interval time.Duration, log zerolog.Logger) {
	t := time.NewTicker(interval)
	defer t.Stop()
	failing := false
	for {
		if _, err := p.Pull(ctx); err != nil {
			if !failing && ctx.Err() == nil {
				log.Warn().Err(err).Str("host", p.host).Msg("agent pull failed")
			}
			failing = true
		} else {
			if failing {
				log.Info().Str("host", p.host).Msg("agent pull recovered")
			}
			failing = false
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
