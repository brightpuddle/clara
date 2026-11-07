// Package manager owns the managed agent sessions of one host: it wires the
// host log, projection and journal to per-session runners and re-attaches
// sessions after a restart.
package manager

import (
	"context"
	"database/sql"
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/internal/agents"
	"github.com/brightpuddle/clara/internal/agents/adapter"
	hostlog "github.com/brightpuddle/clara/internal/agents/log"
	"github.com/brightpuddle/clara/internal/agents/projection"
	"github.com/brightpuddle/clara/internal/agents/runner"
)

// ErrNoSession is returned for commands addressed to a session that is not running.
var ErrNoSession = errors.New("no such running session")

type Manager struct {
	ctx      context.Context
	host     string
	j        *runner.Journal
	adapters map[string]adapter.Adapter
	opts     runner.Options
	log      zerolog.Logger

	mu       sync.Mutex
	defaults map[string]string
	runners  map[string]*runner.Runner
	wg       sync.WaitGroup
}

// New opens the host log in db, rebuilds the projection and re-attaches every
// managed session that was open when the core last stopped. ctx bounds all
// sessions; cancelling it stops them (they are re-attached on the next start).
func New(
	ctx context.Context,
	host string,
	db *sql.DB,
	adapters map[string]adapter.Adapter,
	log zerolog.Logger,
) (*Manager, error) {
	l, err := hostlog.New(db)
	if err != nil {
		return nil, err
	}
	proj, err := projection.Rebuild(ctx, l)
	if err != nil {
		return nil, err
	}
	m := &Manager{
		ctx:      ctx,
		host:     host,
		j:        runner.NewJournal(host, l, proj),
		adapters: adapters,
		log:      log,
		runners:  map[string]*runner.Runner{},
	}
	for _, s := range m.j.Sessions() {
		if s.Origin != agents.OriginManaged {
			continue
		}
		if s.State == agents.StateClosed && s.Reason != runner.ReasonShutdown {
			continue
		}
		ad, ok := adapters[s.Agent]
		if !ok {
			log.Warn().Str("session", s.ID).Str("agent", s.Agent).Msg("no adapter; session not restored")
			continue
		}
		err := m.start(runner.Config{
			Agent:     ad,
			Spec:      adapter.OpenSpec{Dir: s.Dir, Model: s.Model, ResumeID: s.NativeID},
			Kind:      s.Kind,
			Title:     s.Title,
			SessionID: s.ID,
			OpenTurn:  s.ActiveTurn,
		})
		if err != nil {
			log.Error().Err(err).Str("session", s.ID).Msg("restore session")
		}
	}
	return m, nil
}

func (m *Manager) start(cfg runner.Config) error {
	cfg.Options = m.opts
	cfg.Log = m.log
	r, err := runner.Start(m.ctx, m.j, cfg)
	if err != nil {
		return err
	}
	m.track(r.SessionID(), r)
	return nil
}

func (m *Manager) runner(id string) (*runner.Runner, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runners[id]
	if !ok {
		return nil, errors.Wrap(ErrNoSession, id)
	}
	return r, nil
}

func (m *Manager) adapter(name string) (adapter.Adapter, error) {
	ad, ok := m.adapters[name]
	if !ok {
		return nil, errors.Newf("unknown agent %q", name)
	}
	return ad, nil
}

// Host returns the host name events are stamped with.
func (m *Manager) Host() string { return m.host }

// Subscribe registers fn for every recorded event; see runner.Journal.Subscribe.
func (m *Manager) Subscribe(fn func(agents.Event)) { m.j.Subscribe(fn) }

// Wait blocks until every runner has stopped (after the context is cancelled).
func (m *Manager) Wait() { m.wg.Wait() }

// Create starts a new managed session and returns its id. If req.Prompt is set
// it is queued as the first prompt.
func (m *Manager) Create(ctx context.Context, req agents.SessionCreate) (string, error) {
	ad, err := m.adapter(req.Agent)
	if err != nil {
		return "", err
	}
	kind := req.Kind
	if kind == "" {
		kind = agents.KindLongLived
	}
	model := req.Model
	if model == "" {
		m.mu.Lock()
		model = m.defaults[req.Agent]
		m.mu.Unlock()
	}
	return m.open(ctx, ad, adapter.OpenSpec{Dir: req.Dir, Model: model}, kind, req.Prompt, req.Title)
}

// Resume adopts an existing native conversation as a managed session.
func (m *Manager) Resume(ctx context.Context, req agents.SessionResume) (string, error) {
	ad, err := m.adapter(req.Agent)
	if err != nil {
		return "", err
	}
	spec := adapter.OpenSpec{ResumeID: req.NativeID}
	if native, err := ad.List(ctx); err == nil {
		for _, n := range native {
			if n.ID == req.NativeID {
				spec.Dir = n.Dir
			}
		}
	}
	return m.open(ctx, ad, spec, agents.KindLongLived, "", "")
}

func (m *Manager) open(
	ctx context.Context, ad adapter.Adapter, spec adapter.OpenSpec, kind agents.SessionKind, prompt, title string,
) (string, error) {
	cfg := runner.Config{
		Agent: ad, Spec: spec, Kind: kind, Title: title, Options: m.opts, Log: m.log,
	}
	r, err := runner.Start(m.ctx, m.j, cfg)
	if err != nil {
		return "", err
	}
	id := r.SessionID()
	m.track(id, r)
	if prompt != "" {
		if _, err := r.Send(prompt, "create"); err != nil {
			return id, err
		}
	}
	return id, nil
}

func (m *Manager) track(id string, r *runner.Runner) {
	m.mu.Lock()
	m.runners[id] = r
	m.mu.Unlock()
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		<-r.Done()
		m.mu.Lock()
		delete(m.runners, id)
		m.mu.Unlock()
	}()
}

// Discover lists resumable native sessions for agent, or for all agents if empty.
func (m *Manager) Discover(ctx context.Context, agent string) ([]adapter.NativeSession, error) {
	if agent != "" {
		ad, err := m.adapter(agent)
		if err != nil {
			return nil, err
		}
		return ad.List(ctx)
	}
	var out []adapter.NativeSession
	for name, ad := range m.adapters {
		l, err := ad.List(ctx)
		if err != nil {
			return nil, errors.Wrap(err, name)
		}
		out = append(out, l...)
	}
	return out, nil
}

func (m *Manager) List() []projection.Session { return m.j.Sessions() }

func (m *Manager) Get(id string) (projection.Session, bool) { return m.j.Session(id) }

func (m *Manager) Queue(id string) []projection.Prompt { return m.j.Queue(id) }

func (m *Manager) Attentions() []projection.Attention { return m.j.OpenAttentions() }

// ReadEvents returns logged events after host_seq. An empty session reads all.
func (m *Manager) ReadEvents(
	ctx context.Context, session string, after int64, limit int,
) ([]agents.Event, error) {
	return m.j.ReadEvents(ctx, session, after, limit)
}

func (m *Manager) Send(session, text, origin string) (string, error) {
	r, err := m.runner(session)
	if err != nil {
		return "", err
	}
	return r.Send(text, origin)
}

func (m *Manager) CancelPrompt(session, promptID string) error {
	r, err := m.runner(session)
	if err != nil {
		return err
	}
	return r.CancelPrompt(promptID)
}

func (m *Manager) Interrupt(ctx context.Context, session string) error {
	r, err := m.runner(session)
	if err != nil {
		return err
	}
	return r.Interrupt(ctx)
}

// SetModel switches the model of a running session.
func (m *Manager) SetModel(ctx context.Context, session, model string) error {
	r, err := m.runner(session)
	if err != nil {
		return err
	}
	return r.SetModel(ctx, model)
}

// SetDefaultModels sets the model used per agent type when Create gets none.
func (m *Manager) SetDefaultModels(models map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defaults = models
}

func (m *Manager) Close(ctx context.Context, session string) error {
	r, err := m.runner(session)
	if err != nil {
		return err
	}
	return r.Close(ctx)
}

// Clear completely reinitializes the agent session: resets prompt context,
// starts a fresh native agent session on the same directory/settings, and records
// session.cleared.
func (m *Manager) Clear(ctx context.Context, session string) error {
	r, err := m.runner(session)
	if err != nil {
		return err
	}
	return r.Clear(ctx)
}
