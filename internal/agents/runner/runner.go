package runner

import (
	"context"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/internal/agents"
	"github.com/brightpuddle/clara/internal/agents/adapter"
)

// ErrStopped is returned by commands sent to a runner that has finished.
var ErrStopped = errors.New("session runner stopped")

// ReasonShutdown marks a session closed only because the core stopped; such a
// session is re-attached on the next start.
const ReasonShutdown = "shutdown"

// ErrNotQueued is returned when cancelling a prompt that is not in the queue.
var ErrNotQueued = errors.New("prompt is not queued")

type Options struct {
	BackoffInitial time.Duration // delay before the first restart (default 1s)
	BackoffMax     time.Duration // cap on the exponential delay (default 30s)
	MaxRestarts    int           // consecutive failures before giving up (default 5)
	StableAfter    time.Duration // uptime after which the failure count resets (default 30s)
	CloseTimeout   time.Duration // wait for the agent process to exit on close (default 5s)
}

func (o Options) withDefaults() Options {
	if o.BackoffInitial <= 0 {
		o.BackoffInitial = time.Second
	}
	if o.BackoffMax <= 0 {
		o.BackoffMax = 30 * time.Second
	}
	if o.MaxRestarts <= 0 {
		o.MaxRestarts = 5
	}
	if o.StableAfter <= 0 {
		o.StableAfter = 30 * time.Second
	}
	if o.CloseTimeout <= 0 {
		o.CloseTimeout = 5 * time.Second
	}
	return o
}

type Config struct {
	Agent adapter.Adapter
	// Spec says where and how to open. Set Spec.ResumeID to adopt an existing
	// native conversation.
	Spec adapter.OpenSpec
	Kind agents.SessionKind
	// Title is the initial title; the agent may replace it.
	Title string
	// SessionID is set when re-attaching a session already in the log (after a
	// core restart). Empty creates a new session and logs session.created.
	SessionID string
	// OpenTurn is the turn left open in the log by a previous run, if any. It is
	// closed as interrupted and never re-sent.
	OpenTurn string
	Options  Options
	Log      zerolog.Logger
}

type queued struct {
	id, text, origin string
}

type turn struct {
	id, promptID string
	tools        int
	last         []string
}

type openResult struct {
	s   adapter.AgentSession
	err error
}

// Runner owns one session. All state is confined to the loop goroutine; other
// goroutines reach it through do().
type Runner struct {
	id   string
	cfg  Config
	opts Options
	j    *Journal
	log  zerolog.Logger
	bg   context.Context // survives ctx cancellation so final events can be recorded

	cmds chan func()
	done chan struct{}

	// Loop-confined state.
	sess      adapter.AgentSession
	opening   bool
	openedAt  time.Time
	failures  int
	timer     *time.Timer
	state     agents.SessionState
	queue     []queued
	active    *turn
	lastTurn  string
	lastText  string
	blocked   *agents.Blocked
	attention string // open turn_ended attention id
	closing   bool
	shutdown  bool // closing because ctx was cancelled, not because of Close
	nativeID  string
}

// Start registers the session in the log and begins supervising it. The agent
// opens in the background; ctx bounds everything the runner starts.
func Start(ctx context.Context, j *Journal, cfg Config) (*Runner, error) {
	r := &Runner{
		id:       cfg.SessionID,
		cfg:      cfg,
		opts:     cfg.Options.withDefaults(),
		j:        j,
		log:      cfg.Log,
		bg:       context.WithoutCancel(ctx),
		cmds:     make(chan func()),
		done:     make(chan struct{}),
		state:    agents.StateStarting,
		nativeID: cfg.Spec.ResumeID,
	}
	if r.id == "" {
		r.id = agents.NewID()
		_, err := j.Record(ctx, r.id, agents.TypeSessionCreated, agents.SessionCreated{
			Agent: cfg.Agent.Name(), Dir: cfg.Spec.Dir, Title: cfg.Title, Kind: cfg.Kind,
			Model: cfg.Spec.Model, Origin: agents.OriginManaged, NativeID: cfg.Spec.ResumeID,
		})
		if err != nil {
			return nil, err
		}
	} else {
		if cfg.OpenTurn != "" {
			r.recordTurnEnd(cfg.OpenTurn, agents.StopInterrupted, nil, &turn{id: cfg.OpenTurn})
		}
		r.record(agents.TypeSessionStatus, agents.SessionStatus{
			State: agents.StateStarting, Reason: "resuming",
		})
	}
	go r.loop(ctx)
	return r, nil
}

func (r *Runner) SessionID() string { return r.id }

// Done is closed when the runner has stopped.
func (r *Runner) Done() <-chan struct{} { return r.done }

// do runs fn on the loop goroutine and waits for it.
func (r *Runner) do(fn func()) error {
	finished := make(chan struct{})
	select {
	case r.cmds <- func() { fn(); close(finished) }:
		<-finished
		return nil
	case <-r.done:
		return ErrStopped
	}
}

// Send queues a prompt and returns its id. It runs immediately if the session is
// idle (or blocked, where an explicit user prompt retries).
func (r *Runner) Send(text, origin string) (string, error) {
	var id string
	var err error
	if derr := r.do(func() {
		if r.closing {
			err = ErrStopped
			return
		}
		id = agents.NewID()
		r.record(agents.TypePromptQueued, agents.PromptQueued{
			PromptID: id, Text: text, Position: len(r.queue), Origin: origin,
		})
		r.queue = append(r.queue, queued{id: id, text: text, origin: origin})
		r.dispatch(true)
	}); derr != nil {
		return "", derr
	}
	return id, err
}

// CancelPrompt removes a queued (not yet sent) prompt.
func (r *Runner) CancelPrompt(id string) error {
	var err error
	if derr := r.do(func() {
		for i, q := range r.queue {
			if q.id == id {
				r.queue = append(r.queue[:i:i], r.queue[i+1:]...)
				r.record(agents.TypePromptCancelled, agents.PromptCancelled{PromptID: id})
				return
			}
		}
		err = ErrNotQueued
	}); derr != nil {
		return derr
	}
	return err
}

// Interrupt cancels the running turn. The queue is left alone, so the next
// queued prompt runs when the turn ends.
func (r *Runner) Interrupt(ctx context.Context) error {
	var err error
	if derr := r.do(func() {
		if r.active != nil && r.sess != nil {
			err = r.sess.Cancel(ctx)
		}
	}); derr != nil {
		return derr
	}
	return err
}

// Close stops the agent and the runner, then waits for it to finish or ctx to end.
// ErrTurnActive is returned when a model change is attempted mid-turn.
var ErrTurnActive = errors.New("a turn is running; interrupt it or wait before changing model")

// SetModel switches the session's model. It only applies while no turn runs and
// is remembered so restarts re-open the session with the new model.
func (r *Runner) SetModel(ctx context.Context, model string) error {
	var err error
	if derr := r.do(func() {
		ms, ok := r.sess.(adapter.ModelSetter)
		switch {
		case r.sess == nil:
			err = errors.New("session is not open")
		case r.active != nil:
			err = ErrTurnActive
		case !ok:
			err = errors.New("this agent cannot change model")
		default:
			if err = ms.SetModel(ctx, model); err == nil {
				r.cfg.Spec.Model = model
				r.record(agents.TypeSessionUpdated, agents.SessionUpdated{Model: model})
			}
		}
	}); derr != nil {
		return derr
	}
	return err
}

func (r *Runner) Close(ctx context.Context) error {
	_ = r.do(func() { r.beginClose() })
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runner) loop(ctx context.Context) {
	defer close(r.done)
	ctxDone := ctx.Done()
	openCh := make(chan openResult, 1)
	var retry <-chan time.Time

	r.startOpen(ctx, openCh)
	for {
		if r.closing && r.sess == nil && !r.opening {
			r.finish()
			return
		}
		var evs <-chan adapter.AgentEvent
		if r.sess != nil {
			evs = r.sess.Events()
		}
		retry = nil
		if r.timer != nil {
			retry = r.timer.C
		}

		select {
		case <-ctxDone:
			ctxDone = nil
			if !r.closing {
				r.shutdown = true
			}
			r.beginClose()
		case fn := <-r.cmds:
			fn()
		case res := <-openCh:
			r.opening = false
			r.onOpened(res)
		case <-retry:
			r.timer = nil
			if !r.closing {
				r.startOpen(ctx, openCh)
			}
		case ev, ok := <-evs:
			if !ok {
				ev = adapter.Exited{Err: errors.New("event stream ended")}
			}
			r.onEvent(ev)
		}
	}
}

func (r *Runner) startOpen(ctx context.Context, ch chan<- openResult) {
	r.opening = true
	spec := r.cfg.Spec
	spec.ResumeID = r.nativeID
	go func() {
		s, err := r.cfg.Agent.Open(ctx, spec)
		ch <- openResult{s, err}
	}()
}

func (r *Runner) onOpened(res openResult) {
	if res.err != nil {
		if !r.closing {
			r.scheduleRestart(errors.Wrap(res.err, "open"))
		}
		return
	}
	if r.closing {
		r.sess = res.s
		go func(s adapter.AgentSession) { _ = s.Close(r.bg) }(res.s)
		return
	}
	r.sess = res.s
	r.openedAt = time.Now()
	if id := res.s.NativeID(); id != "" && id != r.nativeID {
		r.nativeID = id
		r.record(agents.TypeSessionUpdated, agents.SessionUpdated{NativeID: id})
	}
	r.blocked = nil
	r.setState(agents.StateIdle, "", nil)
	r.dispatch(false)
}

func (r *Runner) scheduleRestart(cause error) {
	r.failures++
	if r.failures > r.opts.MaxRestarts {
		r.setState(agents.StateFailed, "giving up: "+cause.Error(), nil)
		return
	}
	delay := r.opts.BackoffInitial << (r.failures - 1)
	if delay > r.opts.BackoffMax || delay <= 0 {
		delay = r.opts.BackoffMax
	}
	r.setState(agents.StateFailed, cause.Error(), nil)
	r.log.Warn().Err(cause).Str("session", r.id).Dur("retry_in", delay).Msg("agent failed")
	r.timer = time.NewTimer(delay)
}

func (r *Runner) beginClose() {
	if r.closing {
		return
	}
	r.closing = true
	if r.timer != nil {
		r.timer.Stop()
		r.timer = nil
	}
	if r.sess != nil {
		s := r.sess
		// Close in the background: the loop keeps draining events until Exited.
		go func() {
			ctx, cancel := context.WithTimeout(r.bg, r.opts.CloseTimeout)
			defer cancel()
			_ = s.Close(ctx)
		}()
	}
}

func (r *Runner) finish() {
	if r.active != nil {
		r.endTurn(agents.StopInterrupted, nil)
	}
	r.resolveAttention("auto")
	reason := ""
	if r.shutdown {
		reason = ReasonShutdown
	}
	r.setState(agents.StateClosed, reason, nil)
}

func (r *Runner) onEvent(ev adapter.AgentEvent) {
	switch e := ev.(type) {
	case adapter.MessageDelta:
		r.record(agents.TypeMessageDelta, agents.MessageDelta{MessageID: e.MessageID, Text: e.Text})
	case adapter.MessageDone:
		r.lastText = e.Text
		r.record(agents.TypeMessageAssistant, agents.MessageAssistant{
			MessageID: e.MessageID, TurnID: r.currentTurnID(), Text: e.Text,
		})
	case adapter.ToolCall:
		if r.active != nil {
			r.active.tools++
			title := e.Title
			if title == "" {
				title = e.Name
			}
			r.active.last = append(r.active.last, title)
			if len(r.active.last) > 3 {
				r.active.last = r.active.last[1:]
			}
		}
		r.record(agents.TypeToolCall, agents.ToolCall{
			CallID: e.CallID, TurnID: r.currentTurnID(), Name: e.Name, Kind: e.Kind,
			Title: e.Title, Locations: e.Locations, Status: e.Status,
		})
	case adapter.ToolUpdate:
		r.record(agents.TypeToolUpdate, agents.ToolUpdate{
			CallID: e.CallID, Status: e.Status, OutputSummary: e.OutputSummary,
		})
	case adapter.UsageUpdate:
		r.record(agents.TypeUsageUpdated, agents.UsageUpdated{
			Cost: e.Cost, Tokens: agents.Usage{TotalTokens: e.Used},
		})
	case adapter.TitleUpdate:
		r.record(agents.TypeSessionUpdated, agents.SessionUpdated{Title: e.Title})
	case adapter.Blocked:
		b := e.Blocked
		r.blocked = &b
		r.setState(agents.StateBlocked, b.Kind, &b)
	case adapter.TurnEnded:
		if r.active == nil || r.active.promptID != e.PromptID {
			return // stale answer for a prompt we are no longer waiting on
		}
		reason := ""
		if e.StopReason == agents.StopError {
			reason = e.Err
		}
		r.endTurnWith(e.StopReason, e.Usage, reason)
	case adapter.Exited:
		r.onExited(e)
	}
}

func (r *Runner) onExited(e adapter.Exited) {
	r.sess = nil
	if r.closing {
		return
	}
	if r.active != nil {
		r.endTurn(agents.StopInterrupted, nil) // never re-sent
	}
	if !r.openedAt.IsZero() && time.Since(r.openedAt) >= r.opts.StableAfter {
		r.failures = 0
	}
	cause := e.Err
	if cause == nil {
		cause = errors.New("agent exited")
	}
	r.scheduleRestart(cause)
}

// dispatch sends the next queued prompt if the session can take it. Automatic
// dispatch (user=false) does not retry on a blocked session; a new user prompt does.
func (r *Runner) dispatch(user bool) {
	if r.sess == nil || r.active != nil || len(r.queue) == 0 || r.closing {
		return
	}
	switch r.state {
	case agents.StateIdle:
	case agents.StateBlocked:
		if !user {
			return
		}
	default:
		return
	}
	q := r.queue[0]
	r.queue = r.queue[1:]

	t := &turn{id: agents.NewID(), promptID: q.id}
	r.active = t
	r.lastTurn = t.id
	r.blocked = nil
	r.record(agents.TypePromptSent, agents.PromptSent{PromptID: q.id})
	r.record(agents.TypeMessageUser, agents.MessageUser{
		MessageID: agents.NewID(), Text: q.text, Origin: q.origin, PromptID: q.id,
	})
	r.record(agents.TypeTurnStarted, agents.TurnStarted{TurnID: t.id, PromptID: q.id})
	r.resolveAttention("auto")
	r.setState(agents.StateRunning, "", nil)

	if err := r.sess.Prompt(r.bg, adapter.Prompt{ID: q.id, Text: q.text}); err != nil {
		r.endTurnWith(agents.StopError, nil, "send prompt: "+err.Error())
	}
}

func (r *Runner) endTurn(reason agents.StopReason, usage *agents.Usage) {
	r.endTurnWith(reason, usage, "")
}

// endTurnWith closes the active turn, then sets state and attention, and
// continues with the queue if there is anything to send.
func (r *Runner) endTurnWith(reason agents.StopReason, usage *agents.Usage, errText string) {
	t := r.active
	r.active = nil
	r.recordTurnEnd(t.id, reason, usage, t)
	if r.closing {
		return
	}
	if reason == agents.StopInterrupted {
		return // the session is down; state is set by the failure path
	}

	switch {
	case r.blocked != nil:
		r.setState(agents.StateBlocked, r.blocked.Kind, r.blocked)
		return
	case len(r.queue) > 0:
		r.setState(agents.StateIdle, "", nil)
		r.dispatch(false)
		return
	}
	r.setState(agents.StateIdle, "", nil)
	switch reason {
	case agents.StopEndTurn:
		r.requestAttention(firstLine(r.lastText, "Turn ended"))
	case agents.StopError:
		r.requestAttention("Turn failed: " + errText)
	}
}

func (r *Runner) recordTurnEnd(
	id string, reason agents.StopReason, usage *agents.Usage, t *turn,
) {
	r.record(agents.TypeTurnEnded, agents.TurnEnded{
		TurnID: id, StopReason: reason, Usage: usage,
		ToolsSummary: agents.ToolsSummary{Count: t.tools, Last: t.last},
	})
}

func (r *Runner) requestAttention(title string) {
	r.resolveAttention("auto")
	r.attention = agents.NewID()
	r.record(agents.TypeAttentionRequested, agents.AttentionRequested{
		AttentionID: r.attention, Kind: agents.AttentionTurnEnded, Title: title, AllowText: true,
	})
}

func (r *Runner) resolveAttention(by string) {
	if r.attention == "" {
		return
	}
	r.record(agents.TypeAttentionResolved, agents.AttentionResolved{
		AttentionID: r.attention, By: by,
	})
	r.attention = ""
}

func (r *Runner) setState(s agents.SessionState, reason string, b *agents.Blocked) {
	if r.state == s && reason == r.lastReason() {
		return
	}
	r.state = s
	r.record(agents.TypeSessionStatus, agents.SessionStatus{State: s, Reason: reason, Blocked: b})
}

func (r *Runner) lastReason() string {
	sess, ok := r.j.Session(r.id)
	if !ok {
		return ""
	}
	return sess.Reason
}

func (r *Runner) currentTurnID() string {
	if r.active != nil {
		return r.active.id
	}
	return r.lastTurn
}

func (r *Runner) record(typ agents.EventType, payload any) {
	if _, err := r.j.Record(r.bg, r.id, typ, payload); err != nil {
		r.log.Error().Err(err).Str("session", r.id).Str("type", string(typ)).Msg("record event")
	}
}

func firstLine(text, fallback string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return fallback
	}
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}
	if len(text) > 120 {
		text = text[:120] + "…"
	}
	return text
}
