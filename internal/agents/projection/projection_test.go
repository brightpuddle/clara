package projection_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/internal/agents"
	hostlog "github.com/brightpuddle/clara/internal/agents/log"
	"github.com/brightpuddle/clara/internal/agents/projection"
	"github.com/brightpuddle/clara/internal/store"
)

var seq int64

func mk(t *testing.T, session string, typ agents.EventType, p any) agents.Event {
	t.Helper()
	e, err := agents.NewEvent("eve", session, typ, p)
	if err != nil {
		t.Fatal(err)
	}
	seq++
	e.HostSeq = seq
	return e
}

func apply(t *testing.T, p *projection.Projection, evs ...agents.Event) {
	t.Helper()
	for _, e := range evs {
		if err := p.Apply(e); err != nil {
			t.Fatal(err)
		}
	}
}

func created(t *testing.T, s string) agents.Event {
	return mk(t, s, agents.TypeSessionCreated, agents.SessionCreated{
		Agent: "agy", Dir: "/work", Title: "t", Kind: agents.KindLongLived,
		Origin: agents.OriginManaged,
	})
}

func status(t *testing.T, s string, st agents.SessionState) agents.Event {
	return mk(t, s, agents.TypeSessionStatus, agents.SessionStatus{State: st})
}

func TestPruneClosedSessions(t *testing.T) {
	p := projection.New()
	p.SetMaxClosedSessions(2)

	// Create and close 4 sessions
	for i := 1; i <= 4; i++ {
		sid := string(rune('a' + i - 1))
		apply(t, p,
			created(t, sid),
			status(t, sid, agents.StateClosed),
		)
	}

	// Should only have 2 closed sessions retained (c and d, the newest ones)
	sessions := p.Sessions()
	if len(sessions) != 2 {
		t.Fatalf("expected 2 closed sessions retained, got %d", len(sessions))
	}
	if sessions[0].ID != "c" || sessions[1].ID != "d" {
		t.Fatalf("expected sessions c and d, got %s and %s", sessions[0].ID, sessions[1].ID)
	}
}

func TestSessionLifecycle(t *testing.T) {
	p := projection.New()
	apply(t, p,
		created(t, "s1"),
		status(t, "s1", agents.StateIdle),
		mk(t, "s1", agents.TypeSessionUpdated, agents.SessionUpdated{Title: "renamed"}),
		mk(t, "s1", agents.TypeTurnStarted, agents.TurnStarted{TurnID: "t1"}),
		status(t, "s1", agents.StateRunning),
	)
	s, ok := p.Session("s1")
	if !ok {
		t.Fatal("missing session")
	}
	if s.State != agents.StateRunning || s.Title != "renamed" || s.Agent != "agy" ||
		s.ActiveTurn != "t1" {
		t.Fatalf("%+v", s)
	}
	apply(t, p, mk(t, "s1", agents.TypeTurnEnded, agents.TurnEnded{
		TurnID: "t1", StopReason: agents.StopEndTurn}))
	s, _ = p.Session("s1")
	if s.ActiveTurn != "" || s.LastStopReason != agents.StopEndTurn {
		t.Fatalf("%+v", s)
	}
	if s.LastSeq != seq {
		t.Fatalf("last seq %d want %d", s.LastSeq, seq)
	}
}

func TestBlockedStatusCarriesReason(t *testing.T) {
	p := projection.New()
	apply(t, p, created(t, "s1"), mk(t, "s1", agents.TypeSessionStatus, agents.SessionStatus{
		State: agents.StateBlocked, Reason: "quota",
		Blocked: &agents.Blocked{Kind: "quota"},
	}))
	s, _ := p.Session("s1")
	if s.State != agents.StateBlocked || s.Blocked == nil || s.Reason != "quota" {
		t.Fatalf("%+v", s)
	}
	apply(t, p, status(t, "s1", agents.StateIdle))
	s, _ = p.Session("s1")
	if s.Blocked != nil || s.Reason != "" {
		t.Fatalf("blocked not cleared: %+v", s)
	}
}

func TestPromptQueue(t *testing.T) {
	p := projection.New()
	q := func(id string, pos int) agents.Event {
		return mk(t, "s1", agents.TypePromptQueued,
			agents.PromptQueued{PromptID: id, Text: id, Position: pos})
	}
	apply(t, p, created(t, "s1"), q("a", 1), q("b", 2), q("c", 3))
	if got := ids(p.Queue("s1")); got != "abc" {
		t.Fatalf("queue %q", got)
	}
	apply(t, p, mk(t, "s1", agents.TypePromptSent, agents.PromptSent{PromptID: "a"}),
		mk(t, "s1", agents.TypePromptCancelled, agents.PromptCancelled{PromptID: "b"}))
	if got := ids(p.Queue("s1")); got != "c" {
		t.Fatalf("queue %q", got)
	}
	s, _ := p.Session("s1")
	if s.QueueLen != 1 {
		t.Fatalf("queue len %d", s.QueueLen)
	}
}

func ids(ps []projection.Prompt) string {
	out := ""
	for _, p := range ps {
		out += p.ID
	}
	return out
}

func attn(t *testing.T, s, id string, k agents.AttentionKind) agents.Event {
	return mk(t, s, agents.TypeAttentionRequested,
		agents.AttentionRequested{AttentionID: id, Kind: k, Title: id})
}

func TestAttentionResolve(t *testing.T) {
	p := projection.New()
	apply(t, p, created(t, "s1"), attn(t, "s1", "a1", agents.AttentionQuestion))
	if n := len(p.OpenAttentions()); n != 1 {
		t.Fatalf("open %d", n)
	}
	apply(t, p, mk(t, "s1", agents.TypeAttentionResolved,
		agents.AttentionResolved{AttentionID: "a1", By: "dev1", Answer: "yes"}))
	if n := len(p.OpenAttentions()); n != 0 {
		t.Fatalf("open %d", n)
	}
	a, ok := p.Attention("a1")
	if !ok || a.ResolvedBy != "dev1" || a.Answer != "yes" || a.Open {
		t.Fatalf("%+v", a)
	}
}

func TestTurnEndedAttentionAutoResolves(t *testing.T) {
	for name, trigger := range map[string]func(*testing.T) agents.Event{
		"message.user": func(t *testing.T) agents.Event {
			return mk(t, "s1", agents.TypeMessageUser, agents.MessageUser{MessageID: "m", Text: "x"})
		},
		"turn.started": func(t *testing.T) agents.Event {
			return mk(t, "s1", agents.TypeTurnStarted, agents.TurnStarted{TurnID: "t2"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := projection.New()
			apply(t, p, created(t, "s1"),
				attn(t, "s1", "te", agents.AttentionTurnEnded),
				attn(t, "s1", "q", agents.AttentionQuestion))
			apply(t, p, trigger(t))
			open := p.OpenAttentions()
			if len(open) != 1 || open[0].ID != "q" {
				t.Fatalf("open %+v", open)
			}
			a, _ := p.Attention("te")
			if a.ResolvedBy != "auto" {
				t.Fatalf("%+v", a)
			}
		})
	}
}

func TestOpenAttentionsOldestFirst(t *testing.T) {
	p := projection.New()
	apply(t, p, created(t, "s1"), created(t, "s2"),
		attn(t, "s2", "x", agents.AttentionQuestion),
		attn(t, "s1", "y", agents.AttentionQuestion))
	o := p.OpenAttentions()
	if len(o) != 2 || o[0].ID != "x" || o[1].ID != "y" {
		t.Fatalf("%+v", o)
	}
}

func TestApplyIsIdempotentOnSeq(t *testing.T) {
	p := projection.New()
	apply(t, p, created(t, "s1"))
	e := mk(t, "s1", agents.TypePromptQueued,
		agents.PromptQueued{PromptID: "a", Text: "a", Position: 1})
	apply(t, p, e, e)
	if n := len(p.Queue("s1")); n != 1 {
		t.Fatalf("queue %d", n)
	}
}

func TestEventsForUnknownSessionAreTolerated(t *testing.T) {
	p := projection.New()
	apply(t, p, status(t, "ghost", agents.StateIdle))
	if _, ok := p.Session("ghost"); ok {
		t.Fatal("should not invent sessions")
	}
}

func TestClosedSessionDropsQueueAndAttentions(t *testing.T) {
	p := projection.New()
	apply(t, p, created(t, "s1"),
		mk(t, "s1", agents.TypePromptQueued, agents.PromptQueued{PromptID: "a", Position: 1}),
		attn(t, "s1", "q", agents.AttentionQuestion),
		status(t, "s1", agents.StateClosed))
	if len(p.Queue("s1")) != 0 || len(p.OpenAttentions()) != 0 {
		t.Fatal("closed session should have no queue or open attentions")
	}
}

func TestRebuildFromLog(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "clara.db"), zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	l, err := hostlog.New(st.DB())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, e := range []agents.Event{
		created(t, "s1"),
		status(t, "s1", agents.StateIdle),
		mk(t, "s1", agents.TypePromptQueued, agents.PromptQueued{PromptID: "a", Position: 1}),
		attn(t, "s1", "q", agents.AttentionQuestion),
	} {
		if _, err := l.Append(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	p, err := projection.Rebuild(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := p.Session("s1")
	if !ok || s.State != agents.StateIdle || s.QueueLen != 1 || len(p.OpenAttentions()) != 1 {
		t.Fatalf("%+v ok=%v", s, ok)
	}
	if p.Applied() != 4 {
		t.Fatalf("applied %d", p.Applied())
	}
}
