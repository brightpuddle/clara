// Package projection folds the host event log into queryable state: sessions,
// attentions and prompt queues. Projections hold no truth of their own and can
// be dropped and rebuilt from the log at any time.
package projection

import (
	"context"
	"sort"

	"github.com/cockroachdb/errors"

	"github.com/brightpuddle/clara/internal/agents"
	hostlog "github.com/brightpuddle/clara/internal/agents/log"
)

type Session struct {
	ID             string
	Host           string
	Agent          string
	Dir            string
	Title          string
	Model          string
	NativeID       string
	Kind           agents.SessionKind
	Origin         agents.Origin
	State          agents.SessionState
	Reason         string
	Blocked        *agents.Blocked
	ActiveTurn     string
	LastStopReason agents.StopReason
	QueueLen       int
	LastSeq        int64
}

type Prompt struct {
	ID       string
	Session  string
	Text     string
	Origin   string
	QueuedAt int64
}

type Attention struct {
	ID         string
	Session    string
	Kind       agents.AttentionKind
	Title      string
	Options    []agents.Option
	AllowText  bool
	Native     *agents.NativeRef
	Open       bool
	Seq        int64
	ResolvedBy string
	Answer     string
}

// Projection is not safe for concurrent use; callers serialize access.
type Projection struct {
	sessions   map[string]*Session
	queues     map[string][]Prompt
	attentions map[string]*Attention
	applied    int64
}

func New() *Projection {
	return &Projection{
		sessions:   map[string]*Session{},
		queues:     map[string][]Prompt{},
		attentions: map[string]*Attention{},
	}
}

// Rebuild replays the whole log into a fresh Projection.
func Rebuild(ctx context.Context, l *hostlog.Log) (*Projection, error) {
	p := New()
	for {
		evs, err := l.Read(ctx, p.applied, 500)
		if err != nil {
			return nil, err
		}
		if len(evs) == 0 {
			return p, nil
		}
		for _, e := range evs {
			if err := p.Apply(e); err != nil {
				return nil, err
			}
		}
	}
}

// Applied returns the highest HostSeq applied so far.
func (p *Projection) Applied() int64 { return p.applied }

// Apply folds one event in. Events whose HostSeq was already applied are
// skipped, so replays are safe. Events for unknown sessions are ignored.
func (p *Projection) Apply(e agents.Event) error {
	if e.HostSeq > 0 {
		if e.HostSeq <= p.applied {
			return nil
		}
		p.applied = e.HostSeq
	}
	payload, err := e.Decode()
	if err != nil {
		return errors.Wrapf(err, "apply %s", e.Type)
	}

	if c, ok := payload.(agents.SessionCreated); ok {
		p.sessions[e.Session] = &Session{
			ID: e.Session, Host: e.Host, Agent: c.Agent, Dir: c.Dir, Title: c.Title,
			Model: c.Model, NativeID: c.NativeID, Kind: c.Kind, Origin: c.Origin,
			State:   agents.StateStarting,
			LastSeq: e.HostSeq,
		}
		return nil
	}
	s, ok := p.sessions[e.Session]
	if !ok {
		return nil
	}
	if e.HostSeq > 0 {
		s.LastSeq = e.HostSeq
	}

	switch v := payload.(type) {
	case agents.SessionUpdated:
		if v.Title != "" {
			s.Title = v.Title
		}
		if v.Model != "" {
			s.Model = v.Model
		}
		if v.Kind != "" {
			s.Kind = v.Kind
		}
		if v.NativeID != "" {
			s.NativeID = v.NativeID
		}
	case agents.SessionStatus:
		s.State, s.Reason, s.Blocked = v.State, v.Reason, v.Blocked
		if v.State == agents.StateClosed {
			p.closeSession(s)
		}
	case agents.MessageUser:
		p.autoResolve(s.ID)
	case agents.TurnStarted:
		s.ActiveTurn = v.TurnID
		p.autoResolve(s.ID)
	case agents.TurnEnded:
		s.ActiveTurn = ""
		s.LastStopReason = v.StopReason
	case agents.PromptQueued:
		p.queues[s.ID] = append(p.queues[s.ID], Prompt{
			ID: v.PromptID, Session: s.ID, Text: v.Text, Origin: v.Origin, QueuedAt: e.HostSeq,
		})
		s.QueueLen = len(p.queues[s.ID])
	case agents.PromptSent:
		p.dequeue(s, v.PromptID)
	case agents.PromptCancelled:
		p.dequeue(s, v.PromptID)
	case agents.AttentionRequested:
		p.attentions[v.AttentionID] = &Attention{
			ID: v.AttentionID, Session: s.ID, Kind: v.Kind, Title: v.Title,
			Options: v.Options, AllowText: v.AllowText, Native: v.Native,
			Open: true, Seq: e.HostSeq,
		}
	case agents.AttentionResolved:
		if a, ok := p.attentions[v.AttentionID]; ok && a.Open {
			a.Open, a.ResolvedBy, a.Answer = false, v.By, v.Answer
		}
	}
	return nil
}

func (p *Projection) dequeue(s *Session, id string) {
	q := p.queues[s.ID]
	for i, pr := range q {
		if pr.ID == id {
			p.queues[s.ID] = append(q[:i:i], q[i+1:]...)
			break
		}
	}
	s.QueueLen = len(p.queues[s.ID])
}

// autoResolve closes open turn_ended attentions for a session: any new user
// message or turn means the user's move has been taken.
func (p *Projection) autoResolve(session string) {
	for _, a := range p.attentions {
		if a.Session == session && a.Open && a.Kind == agents.AttentionTurnEnded {
			a.Open, a.ResolvedBy = false, "auto"
		}
	}
}

func (p *Projection) closeSession(s *Session) {
	delete(p.queues, s.ID)
	s.QueueLen = 0
	s.ActiveTurn = ""
	for _, a := range p.attentions {
		if a.Session == s.ID && a.Open {
			a.Open, a.ResolvedBy = false, "auto"
		}
	}
}

func (p *Projection) Session(id string) (Session, bool) {
	s, ok := p.sessions[id]
	if !ok {
		return Session{}, false
	}
	return *s, true
}

// Sessions returns all sessions ordered by ID.
func (p *Projection) Sessions() []Session {
	out := make([]Session, 0, len(p.sessions))
	for _, s := range p.sessions {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Queue returns the queued (not yet sent) prompts for a session, in order.
func (p *Projection) Queue(session string) []Prompt {
	return append([]Prompt(nil), p.queues[session]...)
}

func (p *Projection) Attention(id string) (Attention, bool) {
	a, ok := p.attentions[id]
	if !ok {
		return Attention{}, false
	}
	return *a, true
}

// OpenAttentions returns open attentions, oldest first.
func (p *Projection) OpenAttentions() []Attention {
	var out []Attention
	for _, a := range p.attentions {
		if a.Open {
			out = append(out, *a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}
