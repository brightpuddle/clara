package hub

import (
	"context"
	"sync/atomic"

	"github.com/cockroachdb/errors"

	"github.com/brightpuddle/clara/internal/agents"
)

// Source reads a remote host's log by host_seq cursor (agent.events.read).
type Source interface {
	ReadEvents(ctx context.Context, after int64, limit int) ([]agents.Event, error)
}

// Puller drains one host's log into the hub. The cursor is derived from the
// hub log itself, so a restart or reconnect resumes without gaps or repeats.
type Puller struct {
	hub     *Hub
	host    string
	src     Source
	batch   int
	onEvent func([]agents.Event)

	known, online bool
	synced        atomic.Bool
}

// NewPuller returns a Puller for host. onEvent, if set, receives each batch of
// newly stored events (for example to publish them on the event bus).
func NewPuller(
	h *Hub, host string, src Source, batch int, onEvent func([]agents.Event),
) *Puller {
	if batch <= 0 {
		batch = 500
	}
	return &Puller{hub: h, host: host, src: src, batch: batch, onEvent: onEvent}
}

// Pull reads until the host reports no more events and returns how many new
// events were stored. On error the cursor is unchanged for unstored events.
func (p *Puller) Pull(ctx context.Context) (int, error) {
	total := 0
	for {
		after, err := p.hub.HostCursor(ctx, p.host)
		if err != nil {
			return total, err
		}
		evs, err := p.src.ReadEvents(ctx, after, p.batch)
		if err != nil {
			return total, errors.Wrapf(err, "pull from %s", p.host)
		}
		if len(evs) == 0 {
			return total, nil
		}
		for _, e := range evs {
			if e.Host != p.host {
				return total, errors.Newf(
					"host %q returned event for host %q: set agents.host to match", p.host, e.Host)
			}
		}
		stored, err := p.hub.Ingest(ctx, evs)
		if err != nil {
			return total, err
		}
		total += len(stored)
		if len(stored) > 0 && p.onEvent != nil {
			p.onEvent(stored)
		}
		if len(stored) == 0 {
			return total, nil
		}
	}
}

// Synced reports whether the first pull attempt has finished, successfully or
// not. An unreachable host must not hold up clients indefinitely.
func (p *Puller) Synced() bool { return p.synced.Load() }
