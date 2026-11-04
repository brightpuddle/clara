package log_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/internal/agents"
	hostlog "github.com/brightpuddle/clara/internal/agents/log"
	"github.com/brightpuddle/clara/internal/store"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "clara.db"), zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func newLog(t *testing.T) *hostlog.Log {
	t.Helper()
	st := openStore(t)
	l, err := hostlog.New(st.DB())
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func ev(t *testing.T, session string, typ agents.EventType, p any) agents.Event {
	t.Helper()
	e, err := agents.NewEvent("eve", session, typ, p)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestAppendAssignsIncreasingHostSeq(t *testing.T) {
	l := newLog(t)
	ctx := context.Background()
	a, err := l.Append(ctx, ev(t, "s1", agents.TypeTurnStarted, agents.TurnStarted{TurnID: "t1"}))
	if err != nil {
		t.Fatal(err)
	}
	b, err := l.Append(ctx, ev(t, "s1", agents.TypeTurnStarted, agents.TurnStarted{TurnID: "t2"}))
	if err != nil {
		t.Fatal(err)
	}
	if a.HostSeq != 1 || b.HostSeq != 2 {
		t.Fatalf("host_seq = %d, %d", a.HostSeq, b.HostSeq)
	}
}

func TestReadAfterCursor(t *testing.T) {
	l := newLog(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := l.Append(ctx, ev(t, "s1", agents.TypePromptSent, agents.PromptSent{PromptID: "p"})); err != nil {
			t.Fatal(err)
		}
	}
	got, err := l.Read(ctx, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].HostSeq != 3 || got[1].HostSeq != 4 {
		t.Fatalf("got %+v", got)
	}
	got, err = l.Read(ctx, 4, 100)
	if err != nil || len(got) != 1 || got[0].HostSeq != 5 {
		t.Fatalf("got %+v err %v", got, err)
	}
	got, err = l.Read(ctx, 5, 100)
	if err != nil || len(got) != 0 {
		t.Fatalf("expected empty, got %+v err %v", got, err)
	}
}

func TestReadRoundTripsEvent(t *testing.T) {
	l := newLog(t)
	ctx := context.Background()
	in := ev(t, "s9", agents.TypeMessageAssistant,
		agents.MessageAssistant{MessageID: "m", TurnID: "t", Text: "hello"})
	if _, err := l.Append(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, err := l.Read(ctx, 0, 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v err %v", got, err)
	}
	e := got[0]
	if e.ID != in.ID || e.Host != "eve" || e.Session != "s9" || e.Type != in.Type ||
		!e.TS.Equal(in.TS) {
		t.Fatalf("envelope mismatch: %+v vs %+v", e, in)
	}
	p, err := e.Decode()
	if err != nil || p.(agents.MessageAssistant).Text != "hello" {
		t.Fatalf("payload: %v %v", p, err)
	}
}

func TestAppendRejectsDelta(t *testing.T) {
	l := newLog(t)
	_, err := l.Append(context.Background(),
		ev(t, "s", agents.TypeMessageDelta, agents.MessageDelta{MessageID: "m", Text: "x"}))
	if err == nil {
		t.Fatal("delta must not be logged")
	}
}

func TestReadBySession(t *testing.T) {
	l := newLog(t)
	ctx := context.Background()
	l.Append(ctx, ev(t, "a", agents.TypePromptSent, agents.PromptSent{PromptID: "1"}))
	l.Append(ctx, ev(t, "b", agents.TypePromptSent, agents.PromptSent{PromptID: "2"}))
	got, err := l.ReadSession(ctx, "b", 0, 10)
	if err != nil || len(got) != 1 || got[0].Session != "b" {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestLatestSeq(t *testing.T) {
	l := newLog(t)
	ctx := context.Background()
	n, err := l.LatestSeq(ctx)
	if err != nil || n != 0 {
		t.Fatalf("empty: %d %v", n, err)
	}
	l.Append(ctx, ev(t, "a", agents.TypePromptSent, agents.PromptSent{PromptID: "1"}))
	if n, _ = l.LatestSeq(ctx); n != 1 {
		t.Fatalf("latest = %d", n)
	}
}

func TestConcurrentAppendsGapFree(t *testing.T) {
	l := newLog(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := l.Append(ctx, ev(t, "s", agents.TypePromptSent, agents.PromptSent{PromptID: "p"})); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got, err := l.Read(ctx, 0, 100)
	if err != nil || len(got) != 20 {
		t.Fatalf("len %d err %v", len(got), err)
	}
	for i, e := range got {
		if e.HostSeq != int64(i+1) {
			t.Fatalf("gap at %d: %d", i, e.HostSeq)
		}
	}
}

func TestReopenKeepsEvents(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	l1, _ := hostlog.New(st.DB())
	l1.Append(ctx, ev(t, "a", agents.TypePromptSent, agents.PromptSent{PromptID: "1"}))
	l2, err := hostlog.New(st.DB())
	if err != nil {
		t.Fatal(err)
	}
	e, err := l2.Append(ctx, ev(t, "a", agents.TypePromptSent, agents.PromptSent{PromptID: "2"}))
	if err != nil || e.HostSeq != 2 {
		t.Fatalf("seq %d err %v", e.HostSeq, err)
	}
}
