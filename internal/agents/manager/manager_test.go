package manager_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/internal/agents"
	"github.com/brightpuddle/clara/internal/agents/adapter"
	"github.com/brightpuddle/clara/internal/agents/manager"
	"github.com/brightpuddle/clara/internal/store"
)

type fakeSession struct {
	events  chan adapter.AgentEvent
	prompts chan adapter.Prompt
	once    sync.Once
}

func (f *fakeSession) NativeID() string { return "native-1" }
func (f *fakeSession) Prompt(_ context.Context, p adapter.Prompt) error {
	f.prompts <- p
	return nil
}
func (f *fakeSession) Cancel(context.Context) error      { return nil }
func (f *fakeSession) Events() <-chan adapter.AgentEvent { return f.events }
func (f *fakeSession) Close(context.Context) error {
	f.once.Do(func() { f.events <- adapter.Exited{}; close(f.events) })
	return nil
}

type fakeAdapter struct {
	mu       sync.Mutex
	sessions []*fakeSession
	specs    []adapter.OpenSpec
}

func (a *fakeAdapter) Name() string { return "fake" }
func (a *fakeAdapter) List(context.Context) ([]adapter.NativeSession, error) {
	return []adapter.NativeSession{{ID: "n1", Title: "old"}}, nil
}
func (a *fakeAdapter) Open(_ context.Context, spec adapter.OpenSpec) (adapter.AgentSession, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := &fakeSession{events: make(chan adapter.AgentEvent, 16), prompts: make(chan adapter.Prompt, 4)}
	a.sessions = append(a.sessions, s)
	a.specs = append(a.specs, spec)
	return s, nil
}
func (a *fakeAdapter) last() *fakeSession {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessions[len(a.sessions)-1]
}

func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}

func TestCreateSendAndRestore(t *testing.T) {
	db, err := store.OpenMemory(zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	fa := &fakeAdapter{}
	ctx, cancel := context.WithCancel(context.Background())
	m, err := manager.New(ctx, "eve", db.DB(), map[string]adapter.Adapter{"fake": fa}, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}

	id, err := m.Create(ctx, agents.SessionCreate{Agent: "fake", Dir: "/tmp", Kind: agents.KindTask})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(ctx, agents.SessionCreate{Agent: "nope"}); err == nil {
		t.Fatal("expected unknown agent error")
	}
	eventually(t, func() bool {
		s, ok := m.Get(id)
		return ok && s.State == agents.StateIdle
	})

	pid, err := m.Send(id, "hello", "test")
	if err != nil || pid == "" {
		t.Fatalf("send: %q %v", pid, err)
	}
	p := <-fa.last().prompts
	if p.Text != "hello" {
		t.Fatalf("prompt = %q", p.Text)
	}
	fa.last().events <- adapter.TurnEnded{PromptID: p.ID, StopReason: agents.StopEndTurn}
	eventually(t, func() bool { s, _ := m.Get(id); return s.State == agents.StateIdle && s.ActiveTurn == "" })

	evs, err := m.ReadEvents(ctx, id, 0, 100)
	if err != nil || len(evs) == 0 {
		t.Fatalf("events: %d %v", len(evs), err)
	}
	if len(m.List()) != 1 {
		t.Fatalf("list = %d", len(m.List()))
	}
	if _, err := m.Send("missing", "x", "t"); err == nil {
		t.Fatal("expected error for unknown session")
	}

	if err := m.Close(ctx, id); err != nil {
		t.Fatal(err)
	}
	if s, _ := m.Get(id); s.State != agents.StateClosed {
		t.Fatalf("state = %s", s.State)
	}
	cancel()
	m.Wait()
}

func TestRestoreReattachesOpenSessions(t *testing.T) {
	db, _ := store.OpenMemory(zerolog.Nop())
	defer db.Close()
	fa := &fakeAdapter{}
	adapters := map[string]adapter.Adapter{"fake": fa}

	ctx1, cancel1 := context.WithCancel(context.Background())
	m1, _ := manager.New(ctx1, "eve", db.DB(), adapters, zerolog.Nop())
	id, _ := m1.Create(ctx1, agents.SessionCreate{Agent: "fake", Dir: "/w", Kind: agents.KindLongLived})
	eventually(t, func() bool { s, _ := m1.Get(id); return s.State == agents.StateIdle })
	cancel1()
	m1.Wait()

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	m2, err := manager.New(ctx2, "eve", db.DB(), adapters, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { s, _ := m2.Get(id); return s.State == agents.StateIdle })
	fa.mu.Lock()
	spec := fa.specs[len(fa.specs)-1]
	fa.mu.Unlock()
	if spec.ResumeID != "native-1" || spec.Dir != "/w" {
		t.Fatalf("spec = %+v", spec)
	}
}

func TestDiscover(t *testing.T) {
	db, _ := store.OpenMemory(zerolog.Nop())
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m, _ := manager.New(ctx, "eve", db.DB(), map[string]adapter.Adapter{"fake": &fakeAdapter{}}, zerolog.Nop())
	got, err := m.Discover(ctx, "fake")
	if err != nil || len(got) != 1 || got[0].ID != "n1" {
		t.Fatalf("discover = %+v %v", got, err)
	}
}
