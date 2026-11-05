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

// Step runs one pull and, when reachability changes, records and publishes a
// host.status event. The first successful pull reports the host online.
func (p *Puller) Step(ctx context.Context, log zerolog.Logger) {
	_, err := p.Pull(ctx)
	if ctx.Err() != nil {
		return
	}
	defer p.synced.Store(true)
	online := err == nil
	if err != nil && (!p.known || p.online) {
		log.Warn().Err(err).Str("host", p.host).Msg("agent pull failed")
	}
	if p.known && p.online == online {
		return
	}
	if !p.known && !online {
		// Never reached: stay silent until the first success rather than
		// announcing a host as offline that was never online.
		p.known, p.online = true, false
		return
	}
	p.known, p.online = true, online
	ev, aerr := agents.NewEvent(p.host, "", agents.TypeHostStatus,
		agents.HostStatus{Online: online, LastSeen: time.Now().UTC()})
	if aerr == nil {
		ev, aerr = p.hub.Append(ctx, ev)
	}
	if aerr != nil {
		log.Error().Err(aerr).Str("host", p.host).Msg("record host status")
		return
	}
	if p.onEvent != nil {
		p.onEvent([]agents.Event{ev})
	}
}

// Run steps every interval until ctx is cancelled.
func (p *Puller) Run(ctx context.Context, interval time.Duration, log zerolog.Logger) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		p.Step(ctx, log)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
