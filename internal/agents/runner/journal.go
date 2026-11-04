// Package runner drives one managed agent session: prompt queue, turn tracking,
// session state, attention and process supervision. It is agent-agnostic and
// talks to agents only through adapter.AgentSession.
package runner

import (
	"context"
	"sync"

	"github.com/cockroachdb/errors"

	"github.com/brightpuddle/clara/internal/agents"
	hostlog "github.com/brightpuddle/clara/internal/agents/log"
	"github.com/brightpuddle/clara/internal/agents/projection"
)

// Journal is the single write path for host events: it appends to the durable
// log, folds the event into the projection, then fans it out to subscribers.
// Live-only types (message.delta) skip the log and projection.
type Journal struct {
	host string
	log  *hostlog.Log

	mu   sync.Mutex
	proj *projection.Projection
	subs []func(agents.Event)
}

func NewJournal(host string, l *hostlog.Log, p *projection.Projection) *Journal {
	return &Journal{host: host, log: l, proj: p}
}

func (j *Journal) Host() string { return j.host }

// Subscribe registers fn for every recorded event. fn runs under the journal
// lock, in record order, and must not block or call back into the journal.
func (j *Journal) Subscribe(fn func(agents.Event)) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.subs = append(j.subs, fn)
}

// Record builds, persists and publishes one event.
func (j *Journal) Record(
	ctx context.Context, session string, typ agents.EventType, payload any,
) (agents.Event, error) {
	ev, err := agents.NewEvent(j.host, session, typ, payload)
	if err != nil {
		return agents.Event{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if typ.Logged() {
		if ev, err = j.log.Append(ctx, ev); err != nil {
			return agents.Event{}, errors.Wrapf(err, "record %s", typ)
		}
		if err := j.proj.Apply(ev); err != nil {
			return agents.Event{}, err
		}
	}
	for _, fn := range j.subs {
		fn(ev)
	}
	return ev, nil
}

// Session returns the projected state of a session.
func (j *Journal) Session(id string) (projection.Session, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.proj.Session(id)
}

// OpenAttentions returns attentions awaiting the user.
func (j *Journal) OpenAttentions() []projection.Attention {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.proj.OpenAttentions()
}

// Sessions returns every projected session ordered by ID.
func (j *Journal) Sessions() []projection.Session {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.proj.Sessions()
}

// Queue returns the queued prompts of a session.
func (j *Journal) Queue(session string) []projection.Prompt {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.proj.Queue(session)
}

// ReadEvents reads logged events of a session (or all sessions when session is
// empty) after the given host_seq.
func (j *Journal) ReadEvents(
	ctx context.Context, session string, after int64, limit int,
) ([]agents.Event, error) {
	if session == "" {
		return j.log.Read(ctx, after, limit)
	}
	return j.log.ReadSession(ctx, session, after, limit)
}
