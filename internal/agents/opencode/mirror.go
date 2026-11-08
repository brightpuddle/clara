package opencode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog/log"

	"github.com/brightpuddle/clara/internal/agents"
)

// AgentName is the agent label of mirrored sessions.
const AgentName = "opencode"
const agentName = AgentName

// Recorder is the write side of the host log; runner.Journal satisfies it.
type Recorder interface {
	Record(
		ctx context.Context, session string, typ agents.EventType, payload any,
	) (agents.Event, error)
}

type Config struct {
	Host      string
	StateFile string // defaults to DefaultStateFile()
	Recorder  Recorder
	// Seen maps a session id to the message ids already in the Clara log. A
	// present key means session.created was already recorded.
	Seen map[string]map[string]bool
	// OpenForms maps form ids with an open attention in the log to their session.
	OpenForms map[string]string
	// Filter limits which not-yet-registered sessions are mirrored (for example
	// to recently active ones). Registered sessions are always followed.
	Filter func(SessionInfo) bool
	// Store persists Seen across Clara restarts; optional.
	Store   Store
	HTTP    *http.Client
	Backoff time.Duration               // reconnect delay; defaults to 2s
	Start   func(context.Context) error // starts the service when absent; optional
}

// Mirror keeps the Clara log in step with the opencode service and relays
// prompts and form replies back. Session ids are opencode's native ids.
// SessionInfo describes a service session for Config.Filter.
type SessionInfo struct {
	ID      string
	Title   string
	Dir     string
	Updated time.Time
}

type Mirror struct {
	cfg Config
	c   *client

	work sync.Mutex // serialises event handling and CreateSession; guards sess
	sess map[string]*sessState

	mu   sync.Mutex // guards atts and pending
	atts map[string]*attention

	// pending holds prompts sent by Clara clients awaiting their echo from the
	// service, so the mirrored user message keeps the sender's origin.
	pending map[string][]pendingSend
}

type pendingSend struct{ text, origin string }

type sessState struct {
	seen       map[string]bool
	activeTurn string
	tools      []string
	closed     bool
}

type attention struct {
	session string
	fields  []formField
	open    bool
}

func New(cfg Config) *Mirror {
	if cfg.StateFile == "" {
		cfg.StateFile = DefaultStateFile()
	}
	if cfg.Backoff <= 0 {
		cfg.Backoff = 2 * time.Second
	}
	hc := cfg.HTTP
	if hc == nil {
		hc = &http.Client{}
	}
	m := &Mirror{
		cfg:     cfg,
		c:       &client{stateFile: cfg.StateFile, start: cfg.Start, hc: hc},
		sess:    map[string]*sessState{},
		atts:    map[string]*attention{},
		pending: map[string][]pendingSend{},
	}
	for id, s := range cfg.Seen {
		seen := map[string]bool{}
		for mid := range s {
			seen[mid] = true
		}
		m.sess[id] = &sessState{seen: seen}
	}
	for form, s := range cfg.OpenForms {
		m.atts[form] = &attention{session: s, open: true}
	}
	return m
}

// Run mirrors until ctx ends, reconnecting with fresh credentials after any
// failure. It returns ctx.Err().
func (m *Mirror) Run(ctx context.Context) error {
	if m.cfg.Store != nil {
		seen, err := m.cfg.Store.LoadSeen(ctx)
		if err != nil {
			return err
		}
		for id, ids := range seen {
			st := m.sess[id]
			if st == nil {
				st = &sessState{seen: map[string]bool{}}
				m.sess[id] = st
			}
			for mid := range ids {
				st.seen[mid] = true
			}
		}
	}
	for ctx.Err() == nil {
		err := m.c.stream(ctx, "/api/event", func(data []byte) error { return m.onEvent(ctx, data) })
		if ctx.Err() != nil {
			break
		}
		log.Warn().Err(err).Msg("opencode mirror: stream lost, reconnecting")
		m.c.forget()
		select {
		case <-ctx.Done():
		case <-time.After(m.cfg.Backoff):
		}
	}
	return ctx.Err()
}

// Send submits a prompt to a session. The resulting user message is mirrored
// into the log when opencode delivers it (tagged with origin, so chat relays
// can tell it from a prompt typed in the TUI), so Send records nothing itself.
func (m *Mirror) Send(ctx context.Context, session, text, origin string) error {
	m.mu.Lock()
	m.pending[session] = append(m.pending[session], pendingSend{strings.TrimSpace(text), origin})
	m.mu.Unlock()
	err := m.c.do(ctx, http.MethodPost, "/api/session/"+url.PathEscape(session)+"/prompt",
		map[string]any{"text": text}, nil)
	if err != nil {
		m.takePending(session, strings.TrimSpace(text))
	}
	return err
}

// takePending removes and returns the oldest pending send with this text.
func (m *Mirror) takePending(session, text string) (pendingSend, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	q := m.pending[session]
	for i, p := range q {
		if p.text == text {
			m.pending[session] = append(q[:i:i], q[i+1:]...)
			return p, true
		}
	}
	return pendingSend{}, false
}

// CreateSession starts a new service session and registers it in the log before
// returning, so commands can address it immediately. model is "provider/id" or
// empty for the service default; prompt, if set, is sent as the first prompt.
func (m *Mirror) CreateSession(
	ctx context.Context, dir, title, model, prompt string,
) (string, error) {
	body := map[string]any{"location": map[string]string{"directory": dir}}
	if title != "" {
		body["title"] = title
	}
	if model != "" {
		prov, id, ok := strings.Cut(model, "/")
		if !ok {
			return "", errors.Newf("model %q must be provider/id", model)
		}
		body["model"] = map[string]string{"providerID": prov, "id": id}
	}
	var out struct {
		Data apiSession `json:"data"`
	}
	if err := m.c.do(ctx, http.MethodPost, "/api/session", body, &out); err != nil {
		return "", err
	}
	if out.Data.ID == "" {
		return "", errors.New("opencode created a session without an id")
	}
	m.work.Lock()
	if _, ok := m.sess[out.Data.ID]; !ok {
		m.registerAlways(ctx, out.Data)
	}
	m.work.Unlock()
	if prompt != "" {
		if err := m.Send(ctx, out.Data.ID, prompt, "create"); err != nil {
			return out.Data.ID, err
		}
	}
	return out.Data.ID, nil
}

// Interrupt cancels the running turn of a session.
func (m *Mirror) Interrupt(ctx context.Context, session string) error {
	return m.c.do(ctx, http.MethodPost,
		"/api/session/"+url.PathEscape(session)+"/interrupt", nil, nil)
}

// SetModel switches the session's model (model is "provider/id") and records it.
func (m *Mirror) SetModel(ctx context.Context, session, model string) error {
	prov, id, ok := strings.Cut(model, "/")
	if !ok || prov == "" || id == "" {
		return errors.Newf("model %q must be provider/id", model)
	}
	err := m.c.do(ctx, http.MethodPost, "/api/session/"+url.PathEscape(session)+"/model",
		map[string]any{"model": map[string]string{"providerID": prov, "id": id}}, nil)
	if err != nil {
		return err
	}
	m.record(ctx, session, agents.TypeSessionUpdated, agents.SessionUpdated{Model: model})
	return nil
}

// Close stops mirroring a session in Clara. The opencode session and its
// history are left intact (the TUI keeps working); a running turn is
// interrupted first.
func (m *Mirror) Close(ctx context.Context, session string) error {
	if err := m.Interrupt(ctx, session); err != nil && !isStatus(err, http.StatusNotFound) {
		log.Debug().Err(err).Str("session", session).Msg("opencode mirror: interrupt on close")
	}
	m.work.Lock()
	st := m.sess[session]
	if st == nil {
		st = &sessState{seen: map[string]bool{}}
		m.sess[session] = st
	}
	st.closed = true
	m.work.Unlock()
	m.record(ctx, session, agents.TypeSessionStatus,
		agents.SessionStatus{State: agents.StateClosed, Reason: "closed by clara"})
	return nil
}

// Reply answers a single-field question form. ErrResolved means another client
// answered first; nothing is recorded in that case.
func (m *Mirror) Reply(ctx context.Context, attentionID, answer, by string) error {
	m.mu.Lock()
	a := m.atts[attentionID]
	var session, key string
	var ok bool
	if a != nil && a.open && len(a.fields) == 1 {
		session, key, ok = a.session, a.fields[0].Key, true
	}
	m.mu.Unlock()
	if !ok {
		return errors.Newf("attention %q is not an open single-question form", attentionID)
	}
	err := m.c.do(ctx, http.MethodPost,
		"/api/session/"+url.PathEscape(session)+"/form/"+url.PathEscape(attentionID)+"/reply",
		map[string]any{"answer": map[string]string{key: answer}}, nil)
	if isStatus(err, http.StatusNotFound, http.StatusConflict) {
		return errors.Mark(errors.Wrap(err, "reply lost the race"), ErrResolved)
	}
	if err != nil {
		return err
	}
	m.resolve(ctx, attentionID, by, answer)
	return nil
}

// resolve records the resolution at most once per form.
func (m *Mirror) resolve(ctx context.Context, id, by, answer string) {
	m.mu.Lock()
	a := m.atts[id]
	if a == nil || !a.open {
		m.mu.Unlock()
		return
	}
	a.open = false
	session := a.session
	m.mu.Unlock()
	m.record(ctx, session, agents.TypeAttentionResolved,
		agents.AttentionResolved{AttentionID: id, By: by, Answer: answer})
}

func (m *Mirror) markSeen(ctx context.Context, session, id string) {
	if m.cfg.Store == nil {
		return
	}
	if err := m.cfg.Store.MarkSeen(ctx, session, id); err != nil {
		log.Error().Err(err).Str("session", session).Msg("opencode mirror: persist seen failed")
	}
}

func (m *Mirror) record(ctx context.Context, session string, typ agents.EventType, payload any) {
	if _, err := m.cfg.Recorder.Record(ctx, session, typ, payload); err != nil {
		log.Error().Err(err).Str("session", session).Str("type", string(typ)).
			Msg("opencode mirror: record failed")
	}
}

type sseEvent struct {
	Type    string `json:"type"`
	Durable *struct {
		AggregateID string `json:"aggregateID"`
	} `json:"durable"`
	Data json.RawMessage `json:"data"`
}

func (m *Mirror) onEvent(ctx context.Context, raw []byte) error {
	m.work.Lock()
	defer m.work.Unlock()
	var ev sseEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil
	}
	var d struct {
		SessionID          string         `json:"sessionID"`
		AssistantMessageID string         `json:"assistantMessageID"`
		Delta              string         `json:"delta"`
		Form               *form          `json:"form"`
		ID                 string         `json:"id"`
		Answer             map[string]any `json:"answer"`
	}
	_ = json.Unmarshal(ev.Data, &d)

	if st := m.sess[d.SessionID]; st != nil && st.closed {
		return nil
	}
	switch ev.Type {
	case "server.connected":
		return m.sync(ctx)
	case "session.created", "session.execution.started", "session.inbox.delivered",
		"session.step.ended", "session.execution.succeeded", "session.execution.failed",
		"session.execution.interrupted", "session.execution.cancelled":
		return m.refresh(ctx, d.SessionID)
	case "session.text.delta":
		m.record(ctx, d.SessionID, agents.TypeMessageDelta,
			agents.MessageDelta{MessageID: d.AssistantMessageID, Text: d.Delta})
	case "form.created":
		if d.Form != nil {
			m.requestForm(ctx, *d.Form)
		}
	case "form.replied":
		m.resolve(ctx, d.ID, agentName, firstAnswer(d.Answer))
	}
	return nil
}

// refresh registers the session if new and pulls any unseen messages.
func (m *Mirror) refresh(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	if _, ok := m.sess[id]; !ok {
		var out struct {
			Data apiSession `json:"data"`
		}
		if err := m.c.do(ctx, http.MethodGet, "/api/session/"+url.PathEscape(id), nil, &out); err != nil {
			if isStatus(err, http.StatusNotFound) {
				return nil
			}
			return err
		}
		if !m.register(ctx, out.Data) {
			return nil
		}
	}
	return m.catchUp(ctx, id)
}

// register records session.created for a session not seen before. It reports
// whether the session is (now) mirrored.
func (m *Mirror) register(ctx context.Context, s apiSession) bool {
	if _, ok := m.sess[s.ID]; ok {
		return true
	}
	if f := m.cfg.Filter; f != nil && !f(SessionInfo{
		ID: s.ID, Title: s.Title, Dir: s.Location.Directory,
		Updated: time.UnixMilli(s.Time.Updated),
	}) {
		return false
	}
	m.registerAlways(ctx, s)
	return true
}

func (m *Mirror) registerAlways(ctx context.Context, s apiSession) {
	m.sess[s.ID] = &sessState{seen: map[string]bool{}}
	m.markSeen(ctx, s.ID, "")
	m.record(ctx, s.ID, agents.TypeSessionCreated, agents.SessionCreated{
		Agent: agentName, Dir: s.Location.Directory, Title: s.Title,
		Kind: agents.KindLongLived, Model: s.Model.String(),
		Origin: agents.OriginManaged, NativeID: s.ID,
	})
}

// sync reconciles everything after a (re)connect: sessions and history, forms
// that appeared or vanished, and turns that died with a service restart.
func (m *Mirror) sync(ctx context.Context) error {
	var list struct {
		Data []apiSession `json:"data"`
	}
	if err := m.c.do(ctx, http.MethodGet, "/api/session", nil, &list); err != nil {
		return err
	}
	for _, s := range list.Data {
		if st := m.sess[s.ID]; st != nil && st.closed {
			continue
		}
		if !m.register(ctx, s) {
			continue
		}
		if err := m.catchUp(ctx, s.ID); err != nil {
			return err
		}
	}

	var forms struct {
		Data []form `json:"data"`
	}
	if err := m.c.do(ctx, http.MethodGet, "/api/form", nil, &forms); err != nil {
		return err
	}
	live := map[string]bool{}
	for _, f := range forms.Data {
		live[f.ID] = true
		m.requestForm(ctx, f)
	}
	m.mu.Lock()
	var gone []string
	for id, a := range m.atts {
		if a.open && !live[id] {
			gone = append(gone, id)
		}
	}
	m.mu.Unlock()
	for _, id := range gone {
		m.resolve(ctx, id, "cancelled", "")
	}

	var active struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := m.c.do(ctx, http.MethodGet, "/api/session/active", nil, &active); err != nil {
		return err
	}
	for id, st := range m.sess {
		if st.closed {
			continue
		}
		if _, running := active.Data[id]; st.activeTurn != "" && !running {
			m.interrupt(ctx, id, st)
		}
	}
	return nil
}

func (m *Mirror) interrupt(ctx context.Context, id string, st *sessState) {
	m.record(ctx, id, agents.TypeTurnEnded, agents.TurnEnded{
		TurnID: st.activeTurn, StopReason: agents.StopInterrupted,
		ToolsSummary: summarize(st.tools),
	})
	m.record(ctx, id, agents.TypeSessionStatus, agents.SessionStatus{
		State: agents.StateIdle, Reason: "interrupted: service restarted",
	})
	st.activeTurn, st.tools = "", nil
}

func (m *Mirror) requestForm(ctx context.Context, f form) {
	m.mu.Lock()
	if a, ok := m.atts[f.ID]; ok {
		if a.fields == nil {
			a.fields = f.Fields
		}
		m.mu.Unlock()
		return
	}
	m.atts[f.ID] = &attention{session: f.SessionID, fields: f.Fields, open: true}
	m.mu.Unlock()
	m.record(ctx, f.SessionID, agents.TypeAttentionRequested, f.attention())
}
