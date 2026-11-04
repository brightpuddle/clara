package runner_test

import (
	"context"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/internal/agents"
	"github.com/brightpuddle/clara/internal/agents/adapter"
	hostlog "github.com/brightpuddle/clara/internal/agents/log"
	"github.com/brightpuddle/clara/internal/agents/projection"
	"github.com/brightpuddle/clara/internal/agents/runner"
	"github.com/brightpuddle/clara/internal/store"
)

const wait = 5 * time.Second

// fakeSession is a scripted AgentSession.
type fakeSession struct {
	id      string
	events  chan adapter.AgentEvent
	prompts chan adapter.Prompt
	cancels chan struct{}

	mu     sync.Mutex
	busy   bool
	closed bool
}

func (f *fakeSession) NativeID() string { return f.id }

func (f *fakeSession) Prompt(_ context.Context, p adapter.Prompt) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.busy {
		return adapter.ErrBusy
	}
	f.busy = true
	f.prompts <- p
	return nil
}

func (f *fakeSession) Cancel(context.Context) error      { f.cancels <- struct{}{}; return nil }
func (f *fakeSession) Events() <-chan adapter.AgentEvent { return f.events }

func (f *fakeSession) Close(context.Context) error { f.exit(nil); return nil }

func (f *fakeSession) exit(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	f.closed = true
	f.events <- adapter.Exited{Err: err}
	close(f.events)
}

// say emits an event; end-of-turn events also free the session for a prompt.
func (f *fakeSession) say(ev adapter.AgentEvent) {
	if _, ok := ev.(adapter.TurnEnded); ok {
		f.mu.Lock()
		f.busy = false
		f.mu.Unlock()
	}
	f.events <- ev
}

func (f *fakeSession) end(promptID string, reason agents.StopReason) {
	f.say(adapter.TurnEnded{PromptID: promptID, StopReason: reason})
}

func (f *fakeSession) nextPrompt(t *testing.T) adapter.Prompt {
	t.Helper()
	select {
	case p := <-f.prompts:
		return p
	case <-time.After(wait):
		t.Fatal("no prompt sent to agent")
	}
	return adapter.Prompt{}
}

type fakeAdapter struct {
	mu       sync.Mutex
	failures []error // consumed one per Open call
	always   error
	specs    []adapter.OpenSpec
	n        int
	opened   chan *fakeSession
}

func newFakeAdapter() *fakeAdapter { return &fakeAdapter{opened: make(chan *fakeSession, 32)} }

func (a *fakeAdapter) Name() string                                          { return "fake" }
func (a *fakeAdapter) List(context.Context) ([]adapter.NativeSession, error) { return nil, nil }

func (a *fakeAdapter) Open(_ context.Context, spec adapter.OpenSpec) (adapter.AgentSession, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.specs = append(a.specs, spec)
	if a.always != nil {
		return nil, a.always
	}
	if len(a.failures) > 0 {
		err := a.failures[0]
		a.failures = a.failures[1:]
		if err != nil {
			return nil, err
		}
	}
	a.n++
	id := spec.ResumeID
	if id == "" {
		id = "native-1"
	}
	s := &fakeSession{
		id: id, events: make(chan adapter.AgentEvent, 64), prompts: make(chan adapter.Prompt, 8),
		cancels: make(chan struct{}, 8),
	}
	a.opened <- s
	return s, nil
}

func (a *fakeAdapter) session(t *testing.T) *fakeSession {
	t.Helper()
	select {
	case s := <-a.opened:
		return s
	case <-time.After(wait):
		t.Fatal("agent was not opened")
	}
	return nil
}

type harness struct {
	t   *testing.T
	j   *runner.Journal
	log *hostlog.Log
	ad  *fakeAdapter
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "clara.db"), zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	l, err := hostlog.New(st.DB())
	if err != nil {
		t.Fatal(err)
	}
	j := runner.NewJournal("eve", l, projection.New())
	return &harness{t: t, j: j, log: l, ad: newFakeAdapter()}
}

var fast = runner.Options{
	BackoffInitial: 5 * time.Millisecond, BackoffMax: 20 * time.Millisecond,
	MaxRestarts: 3, CloseTimeout: time.Second,
}

func (h *harness) start(cfg runner.Config) *runner.Runner {
	h.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	if cfg.Agent == nil {
		cfg.Agent = h.ad
	}
	if cfg.Spec.Dir == "" {
		cfg.Spec.Dir = "/tmp"
	}
	if cfg.Options == (runner.Options{}) {
		cfg.Options = fast
	}
	r, err := runner.Start(ctx, h.j, cfg)
	if err != nil {
		cancel()
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() {
		cancel()
		select {
		case <-r.Done():
		case <-time.After(wait):
			h.t.Error("runner did not stop")
		}
	})
	return r
}

func (h *harness) types(r *runner.Runner) []string {
	h.t.Helper()
	evs, err := h.log.ReadSession(context.Background(), r.SessionID(), 0, 1000)
	if err != nil {
		h.t.Fatal(err)
	}
	var out []string
	for _, e := range evs {
		out = append(out, string(e.Type))
	}
	return out
}

func (h *harness) events(r *runner.Runner, typ agents.EventType) []agents.Event {
	h.t.Helper()
	evs, _ := h.log.ReadSession(context.Background(), r.SessionID(), 0, 1000)
	var out []agents.Event
	for _, e := range evs {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func (h *harness) until(what string, cond func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(wait)
	for !cond() {
		if time.Now().After(deadline) {
			h.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (h *harness) state(r *runner.Runner) agents.SessionState {
	s, _ := h.j.Session(r.SessionID())
	return s.State
}

func (h *harness) untilState(r *runner.Runner, want agents.SessionState) {
	h.t.Helper()
	h.until("state "+string(want), func() bool { return h.state(r) == want })
}

func startIdle(t *testing.T) (*harness, *runner.Runner, *fakeSession) {
	h := newHarness(t)
	r := h.start(runner.Config{Kind: agents.KindLongLived, Title: "t"})
	s := h.ad.session(t)
	h.untilState(r, agents.StateIdle)
	return h, r, s
}

func TestTurnLifecycleAndAttention(t *testing.T) {
	h, r, s := startIdle(t)
	pid, err := r.Send("hello", "discord")
	if err != nil {
		t.Fatal(err)
	}
	if p := s.nextPrompt(t); p.ID != pid || p.Text != "hello" {
		t.Fatalf("prompt %#v", p)
	}
	h.untilState(r, agents.StateRunning)

	s.say(adapter.MessageDelta{MessageID: "m1", Text: "hi"})
	s.say(adapter.ToolCall{CallID: "c1", Title: "ls"})
	s.say(adapter.MessageDone{MessageID: "m1", Text: "hi there\nmore"})
	s.end(pid, agents.StopEndTurn)
	h.untilState(r, agents.StateIdle)

	want := []string{
		"session.created", "session.updated", "session.status", // created, native id, idle
		"prompt.queued", "prompt.sent", "message.user", "turn.started", "session.status",
		"tool.call", "message.assistant", "turn.ended", "session.status", "attention.requested",
	}
	if got := h.types(r); !slices.Equal(got, want) {
		t.Fatalf("events\n got %v\nwant %v", got, want)
	}
	te, _ := h.events(r, agents.TypeTurnEnded)[0].Decode()
	if sum := te.(agents.TurnEnded).ToolsSummary; sum.Count != 1 || sum.Last[0] != "ls" {
		t.Fatalf("tools summary %#v", sum)
	}
	att := h.j.OpenAttentions()
	if len(att) != 1 || att[0].Kind != agents.AttentionTurnEnded || att[0].Title != "hi there" {
		t.Fatalf("attentions %#v", att)
	}
	if sess, _ := h.j.Session(r.SessionID()); sess.NativeID != "native-1" {
		t.Fatalf("native id %q", sess.NativeID)
	}

	// A new prompt means the user's move was taken.
	if _, err := r.Send("again", "discord"); err != nil {
		t.Fatal(err)
	}
	s.nextPrompt(t)
	if n := len(h.j.OpenAttentions()); n != 0 {
		t.Fatalf("attention should auto-resolve, %d open", n)
	}
}

func TestQueueRunsOnePromptAtATime(t *testing.T) {
	h, r, s := startIdle(t)
	p1, _ := r.Send("one", "")
	p2, _ := r.Send("two", "")
	p3, _ := r.Send("three", "")

	if p := s.nextPrompt(t); p.ID != p1 {
		t.Fatalf("first %#v", p)
	}
	select {
	case p := <-s.prompts:
		t.Fatalf("second prompt sent while first is running: %#v", p)
	case <-time.After(50 * time.Millisecond):
	}
	if sess, _ := h.j.Session(r.SessionID()); sess.QueueLen != 2 {
		t.Fatalf("queue len %d", sess.QueueLen)
	}

	s.end(p1, agents.StopEndTurn)
	if p := s.nextPrompt(t); p.ID != p2 {
		t.Fatalf("second %#v", p)
	}
	if n := len(h.j.OpenAttentions()); n != 0 {
		t.Fatal("no attention while the queue is non-empty")
	}
	s.end(p2, agents.StopEndTurn)
	if p := s.nextPrompt(t); p.ID != p3 {
		t.Fatalf("third %#v", p)
	}
	s.end(p3, agents.StopEndTurn)
	h.until("attention", func() bool { return len(h.j.OpenAttentions()) == 1 })
	if st := h.state(r); st != agents.StateIdle {
		t.Fatalf("state %s", st)
	}
}

func TestCancelQueuedPrompt(t *testing.T) {
	h, r, s := startIdle(t)
	p1, _ := r.Send("one", "")
	p2, _ := r.Send("two", "")
	s.nextPrompt(t)
	if err := r.CancelPrompt(p2); err != nil {
		t.Fatal(err)
	}
	if err := r.CancelPrompt(p2); !errors.Is(err, runner.ErrNotQueued) {
		t.Fatalf("want ErrNotQueued, got %v", err)
	}
	if err := r.CancelPrompt(p1); !errors.Is(err, runner.ErrNotQueued) {
		t.Fatalf("a sent prompt cannot be cancelled this way, got %v", err)
	}
	s.end(p1, agents.StopEndTurn)
	h.untilState(r, agents.StateIdle)
	if n := len(h.events(r, agents.TypePromptCancelled)); n != 1 {
		t.Fatalf("prompt.cancelled events: %d", n)
	}
}

func TestInterruptKeepsQueue(t *testing.T) {
	h, r, s := startIdle(t)
	if err := r.Interrupt(context.Background()); err != nil { // idle: no-op
		t.Fatal(err)
	}
	p1, _ := r.Send("one", "")
	p2, _ := r.Send("two", "")
	s.nextPrompt(t)
	if err := r.Interrupt(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.cancels:
	case <-time.After(wait):
		t.Fatal("agent was not cancelled")
	}
	s.end(p1, agents.StopCancelled)
	if p := s.nextPrompt(t); p.ID != p2 {
		t.Fatalf("queue should continue after an interrupt, got %#v", p)
	}
	s.end(p2, agents.StopCancelled)
	h.untilState(r, agents.StateIdle)
	if n := len(h.j.OpenAttentions()); n != 0 {
		t.Fatalf("a cancelled turn needs no attention, got %d", n)
	}
}

func TestPromptErrorRaisesFailedTurnAttention(t *testing.T) {
	h, r, s := startIdle(t)
	p1, _ := r.Send("one", "")
	s.nextPrompt(t)
	s.say(adapter.TurnEnded{PromptID: p1, StopReason: agents.StopError, Err: "boom"})
	h.until("attention", func() bool { return len(h.j.OpenAttentions()) == 1 })
	if a := h.j.OpenAttentions()[0]; a.Title != "Turn failed: boom" {
		t.Fatalf("title %q", a.Title)
	}
	if h.state(r) != agents.StateIdle {
		t.Fatalf("state %s", h.state(r))
	}
}

func TestStaleTurnEndedIgnored(t *testing.T) {
	h, r, s := startIdle(t)
	p1, _ := r.Send("one", "")
	s.nextPrompt(t)
	s.say(adapter.TurnEnded{PromptID: "old", StopReason: agents.StopEndTurn})
	if _, err := r.Send("sync", ""); err != nil { // round-trips through the loop
		t.Fatal(err)
	}
	if h.state(r) != agents.StateRunning {
		t.Fatalf("state %s", h.state(r))
	}
	s.end(p1, agents.StopEndTurn)
}

func TestBlockedHoldsQueueUntilUserPrompts(t *testing.T) {
	h, r, s := startIdle(t)
	p1, _ := r.Send("one", "")
	r.Send("two", "")
	s.nextPrompt(t)
	reset := time.Now().Add(time.Hour)
	s.say(adapter.Blocked{Blocked: agents.Blocked{Kind: "quota", ResetsAt: &reset}})
	s.end(p1, agents.StopEndTurn)
	h.untilState(r, agents.StateBlocked)

	if sess, _ := h.j.Session(r.SessionID()); sess.Blocked == nil || sess.Blocked.Kind != "quota" {
		t.Fatalf("blocked %#v", sess.Blocked)
	}
	if n := len(h.j.OpenAttentions()); n != 0 {
		t.Fatalf("blocked is not a turn_ended attention, got %d", n)
	}
	select {
	case p := <-s.prompts:
		t.Fatalf("queue must not auto-run while blocked: %#v", p)
	case <-time.After(50 * time.Millisecond):
	}

	p3, _ := r.Send("retry", "device")
	if p := s.nextPrompt(t); p.Text != "two" {
		t.Fatalf("a user prompt retries from the head of the queue, got %#v", p)
	}
	h.untilState(r, agents.StateRunning)
	_ = p3
}

func TestCrashMidTurnInterruptsAndResumes(t *testing.T) {
	h, r, s1 := startIdle(t)
	p1, _ := r.Send("one", "")
	p2, _ := r.Send("two", "")
	s1.nextPrompt(t)

	s1.exit(errors.New("segfault"))
	h.until("interrupted turn", func() bool {
		for _, e := range h.events(r, agents.TypeTurnEnded) {
			v, _ := e.Decode()
			if v.(agents.TurnEnded).StopReason == agents.StopInterrupted {
				return true
			}
		}
		return false
	})

	s2 := h.ad.session(t)
	if got := h.ad.specs[len(h.ad.specs)-1].ResumeID; got != "native-1" {
		t.Fatalf("resume id %q", got)
	}
	// The queued prompt runs on the new process; the interrupted one is not re-sent.
	if p := s2.nextPrompt(t); p.ID != p2 {
		t.Fatalf("got %#v, want queued prompt %s (interrupted %s must not be re-sent)", p, p2, p1)
	}
	s2.end(p2, agents.StopEndTurn)
	h.untilState(r, agents.StateIdle)

	var failed bool
	for _, e := range h.events(r, agents.TypeSessionStatus) {
		v, _ := e.Decode()
		failed = failed || v.(agents.SessionStatus).State == agents.StateFailed
	}
	if !failed {
		t.Fatal("expected a failed status between crash and resume")
	}
}

func TestOpenFailuresBackOffThenGiveUp(t *testing.T) {
	h := newHarness(t)
	h.ad.always = errors.New("no auth")
	r := h.start(runner.Config{Kind: agents.KindTask})
	h.until("give up", func() bool {
		s, _ := h.j.Session(r.SessionID())
		return s.State == agents.StateFailed && len(s.Reason) > 10 && s.Reason[:10] == "giving up:"
	})
	h.ad.mu.Lock()
	n := len(h.ad.specs)
	h.ad.mu.Unlock()
	if n != fast.MaxRestarts+1 {
		t.Fatalf("opens: %d, want %d", n, fast.MaxRestarts+1)
	}
	// Still accepts prompts into the queue.
	if _, err := r.Send("queued", ""); err != nil {
		t.Fatal(err)
	}
}

func TestOpenRecoversAfterTransientFailure(t *testing.T) {
	h := newHarness(t)
	h.ad.failures = []error{errors.New("flaky")}
	r := h.start(runner.Config{Kind: agents.KindTask})
	h.ad.session(t)
	h.untilState(r, agents.StateIdle)
}

func TestCloseStopsAgentAndRunner(t *testing.T) {
	h, r, s := startIdle(t)
	r.Send("one", "")
	s.nextPrompt(t)

	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	if err := r.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if h.state(r) != agents.StateClosed {
		t.Fatalf("state %s", h.state(r))
	}
	last := h.events(r, agents.TypeTurnEnded)
	v, _ := last[len(last)-1].Decode()
	if v.(agents.TurnEnded).StopReason != agents.StopInterrupted {
		t.Fatalf("open turn should end interrupted: %#v", v)
	}
	if _, err := r.Send("late", ""); !errors.Is(err, runner.ErrStopped) {
		t.Fatalf("want ErrStopped, got %v", err)
	}
	if err := r.Close(ctx); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestContextCancelStopsRunner(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	r, err := runner.Start(ctx, h.j, runner.Config{
		Agent: h.ad, Spec: adapter.OpenSpec{Dir: "/tmp"}, Options: fast,
	})
	if err != nil {
		t.Fatal(err)
	}
	h.ad.session(t)
	h.untilState(r, agents.StateIdle)
	cancel()
	select {
	case <-r.Done():
	case <-time.After(wait):
		t.Fatal("runner did not stop")
	}
	if h.state(r) != agents.StateClosed {
		t.Fatalf("state %s", h.state(r))
	}
}

func TestCloseWhileOpenInFlight(t *testing.T) {
	h := newHarness(t)
	h.ad.failures = []error{errors.New("flaky")}
	r := h.start(runner.Config{})
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	if err := r.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if h.state(r) != agents.StateClosed {
		t.Fatalf("state %s", h.state(r))
	}
}

func TestReattachClosesOpenTurnAndSkipsCreate(t *testing.T) {
	h := newHarness(t)
	first := h.start(runner.Config{})
	h.ad.session(t)
	h.untilState(first, agents.StateIdle)
	id := first.SessionID()

	r := h.start(runner.Config{SessionID: id, OpenTurn: "turn-x", Spec: adapter.OpenSpec{
		Dir: "/tmp", ResumeID: "native-1"}})
	h.ad.session(t)
	h.until("reattached", func() bool {
		return len(h.events(r, agents.TypeTurnEnded)) == 1
	})
	if n := len(h.events(r, agents.TypeSessionCreated)); n != 1 {
		t.Fatalf("session.created logged %d times", n)
	}
	v, _ := h.events(r, agents.TypeTurnEnded)[0].Decode()
	te := v.(agents.TurnEnded)
	if te.TurnID != "turn-x" || te.StopReason != agents.StopInterrupted {
		t.Fatalf("turn end %#v", te)
	}
}

func TestDeltasAreLiveOnly(t *testing.T) {
	h, r, s := startIdle(t)
	var got []agents.EventType
	var mu sync.Mutex
	h.j.Subscribe(func(e agents.Event) { mu.Lock(); got = append(got, e.Type); mu.Unlock() })
	p1, _ := r.Send("one", "")
	s.nextPrompt(t)
	s.say(adapter.MessageDelta{MessageID: "m", Text: "x"})
	s.end(p1, agents.StopEndTurn)
	h.untilState(r, agents.StateIdle)
	h.until("delta published", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return slices.Contains(got, agents.TypeMessageDelta)
	})
	if n := len(h.events(r, agents.TypeMessageDelta)); n != 0 {
		t.Fatalf("delta was logged %d times", n)
	}
}

func TestNoGoroutineLeaks(t *testing.T) {
	run := func() {
		h := newHarness(t)
		for i := 0; i < 10; i++ {
			r := h.start(runner.Config{})
			s := h.ad.session(t)
			h.untilState(r, agents.StateIdle)
			if i%2 == 0 {
				pid, _ := r.Send("x", "")
				s.nextPrompt(t)
				_ = pid
			}
			ctx, cancel := context.WithTimeout(context.Background(), wait)
			if err := r.Close(ctx); err != nil {
				t.Fatal(err)
			}
			cancel()
		}
	}
	runtime.GC()
	before := runtime.NumGoroutine()
	run()
	deadline := time.Now().Add(wait)
	for runtime.NumGoroutine() > before+2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before+2 {
		buf := make([]byte, 1<<16)
		n := runtime.Stack(buf, true)
		t.Fatalf("goroutines %d -> %d\n%s", before, after, buf[:n])
	}
}

func TestConcurrentCommandsUnderRace(t *testing.T) {
	h, r, s := startIdle(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := r.Send("x", "")
			if err == nil && i%3 == 0 {
				_ = r.CancelPrompt(id)
			}
			_ = r.Interrupt(context.Background())
		}()
	}
	// Answer whatever reaches the agent.
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case p := <-s.prompts:
				s.end(p.ID, agents.StopEndTurn)
			case <-s.cancels:
			case <-stop:
				return
			}
		}
	}()
	wg.Wait()
	h.until("queue drained", func() bool {
		sess, _ := h.j.Session(r.SessionID())
		return sess.QueueLen == 0 && sess.State == agents.StateIdle
	})
	close(stop)
}
