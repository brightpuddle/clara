package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/internal/agents"
	"github.com/brightpuddle/clara/internal/store"
)

const (
	spikeSession = "ses_e587a6e20ffeXh9TDhjeGDlrE6"
	spikeForm    = "frm_1a7859cce0014Pcz8COnHNbJOL"
	spikeUserMsg = "msg_1a7859223001RvIgSBvCmj2AOQ"
)

// ---- fake recorder ----

type recorded struct {
	Session string
	Type    agents.EventType
	Payload any
}

type fakeRecorder struct {
	mu  sync.Mutex
	evs []recorded
}

func (r *fakeRecorder) Record(
	_ context.Context, session string, typ agents.EventType, payload any,
) (agents.Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evs = append(r.evs, recorded{session, typ, payload})
	return agents.Event{Session: session, Type: typ}, nil
}

func (r *fakeRecorder) snapshot() []recorded {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recorded(nil), r.evs...)
}

func (r *fakeRecorder) types() []agents.EventType {
	var out []agents.EventType
	for _, e := range r.snapshot() {
		out = append(out, e.Type)
	}
	return out
}

func (r *fakeRecorder) of(typ agents.EventType) []recorded {
	var out []recorded
	for _, e := range r.snapshot() {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (r *fakeRecorder) waitType(t *testing.T, typ agents.EventType, n int) {
	t.Helper()
	waitFor(t, fmt.Sprintf("%d x %s (have %v)", n, typ, r.types()), func() bool {
		return len(r.of(typ)) >= n
	})
}

// ---- fake opencode service ----

type fakeService struct {
	t         *testing.T
	srv       *httptest.Server
	stateFile string

	mu       sync.Mutex
	password string
	sessions map[string]json.RawMessage
	messages map[string][]json.RawMessage
	forms    []json.RawMessage
	active   map[string]bool
	streams  []chan string
	logCalls int
	prompts  []string
	replies  []string
	replyErr int

	interrupts []string
	auths      int
}

func newFakeService(t *testing.T) *fakeService {
	t.Helper()
	f := &fakeService{
		t:         t,
		password:  "pw-1",
		sessions:  map[string]json.RawMessage{},
		messages:  map[string][]json.RawMessage{},
		active:    map[string]bool{},
		stateFile: filepath.Join(t.TempDir(), "service.json"),
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	f.writeState()
	return f
}

func (f *fakeService) writeState() {
	f.mu.Lock()
	pw := f.password
	f.mu.Unlock()
	b, _ := json.Marshal(map[string]any{
		"id": "svc", "version": "2.2.6", "url": f.srv.URL, "pid": os.Getpid(), "password": pw,
	})
	if err := os.WriteFile(f.stateFile, b, 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fakeService) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	user, pass, ok := r.BasicAuth()
	if !ok || user != "opencode" || pass != f.password {
		f.mu.Unlock()
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	f.auths++
	f.mu.Unlock()

	path := r.URL.Path
	switch {
	case path == "/api/event":
		f.serveEvents(w, r)
	case strings.HasSuffix(path, "/log"):
		f.mu.Lock()
		f.logCalls++
		f.mu.Unlock()
		http.Error(w, "unused", http.StatusGone)
	case path == "/api/session" && r.Method == http.MethodGet:
		f.mu.Lock()
		var list []json.RawMessage
		for _, s := range f.sessions {
			list = append(list, s)
		}
		f.mu.Unlock()
		writeJSON(w, map[string]any{"data": list})
	case path == "/api/session/active":
		f.mu.Lock()
		m := map[string]any{}
		for id := range f.active {
			m[id] = map[string]string{"type": "running"}
		}
		f.mu.Unlock()
		writeJSON(w, map[string]any{"data": m})
	case path == "/api/form":
		f.mu.Lock()
		forms := append([]json.RawMessage{}, f.forms...)
		f.mu.Unlock()
		writeJSON(w, map[string]any{"data": forms})
	case strings.HasPrefix(path, "/api/session/"):
		f.session(w, r, strings.Split(strings.TrimPrefix(path, "/api/session/"), "/"))
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeService) session(w http.ResponseWriter, r *http.Request, parts []string) {
	id := parts[0]
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case len(parts) == 1:
		s, ok := f.sessions[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, map[string]any{"data": s})
	case parts[1] == "message":
		writeJSON(w, map[string]any{"data": f.messages[id], "cursor": map[string]string{}})
	case parts[1] == "interrupt":
		f.interrupts = append(f.interrupts, id)
		writeJSON(w, map[string]any{})
	case parts[1] == "prompt":
		var b struct{ Text string }
		_ = json.NewDecoder(r.Body).Decode(&b)
		if _, ok := f.sessions[id]; !ok {
			http.NotFound(w, r)
			return
		}
		f.prompts = append(f.prompts, b.Text)
		writeJSON(w, map[string]any{})
	case len(parts) == 4 && parts[1] == "form" && parts[3] == "reply":
		if f.replyErr != 0 {
			w.WriteHeader(f.replyErr)
			return
		}
		body, _ := readAll(r)
		f.replies = append(f.replies, parts[2]+" "+body)
		writeJSON(w, map[string]any{})
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeService) serveEvents(w http.ResponseWriter, r *http.Request) {
	ch := make(chan string, 64)
	f.mu.Lock()
	f.streams = append(f.streams, ch)
	f.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	fl := w.(http.Flusher)
	fmt.Fprint(w, "data: {\"id\":\"evt_c\",\"type\":\"server.connected\",\"data\":{}}\n\n")
	fl.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, open := <-ch:
			if !open {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", ev)
			fl.Flush()
		}
	}
}

func (f *fakeService) push(ev string) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(ev)); err != nil {
		f.t.Fatalf("bad event json: %v", err)
	}
	ev = buf.String()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, ch := range f.streams {
		select {
		case ch <- ev:
		default:
		}
	}
}

func (f *fakeService) streamCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.streams)
}

// restart simulates a service restart: new password, forms and streams gone.
func (f *fakeService) restart() {
	f.mu.Lock()
	f.password = fmt.Sprintf("pw-%d", f.auths+100)
	f.forms = nil
	delete(f.active, spikeSession)
	for _, ch := range f.streams {
		close(ch)
	}
	f.streams = nil
	f.mu.Unlock()
	f.writeState()
}

func (f *fakeService) addSession(id, title, dir string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[id], _ = json.Marshal(map[string]any{
		"id": id, "title": title,
		"location": map[string]string{"directory": dir},
		"model":    map[string]string{"id": "gemini-3.8-flash", "providerID": "github-copilot"},
	})
}

func (f *fakeService) setMessages(id string, msgs ...json.RawMessage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages[id] = msgs
}

func (f *fakeService) appendMessages(id string, msgs ...json.RawMessage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages[id] = append(f.messages[id], msgs...)
}

func (f *fakeService) setForms(forms ...json.RawMessage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.forms = forms
}

func (f *fakeService) setActive(id string, on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if on {
		f.active[id] = true
	} else {
		delete(f.active, id)
	}
}

func (f *fakeService) setReplyErr(code int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replyErr = code
}

func (f *fakeService) logCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.logCalls
}

func (f *fakeService) interruptsSent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.interrupts...)
}

func (f *fakeService) promptsSent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.prompts...)
}

func (f *fakeService) repliesSent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.replies...)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func readAll(r *http.Request) (string, error) {
	var sb strings.Builder
	buf := make([]byte, 512)
	for {
		n, err := r.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			return sb.String(), nil
		}
	}
}

// ---- fixtures ----

func spikeMessages(t *testing.T) []json.RawMessage {
	t.Helper()
	b, err := os.ReadFile("../testdata/opencode/messages_spike.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct{ Data []json.RawMessage }
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Data
}

func spikeFormJSON() json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"id":%q,"sessionID":%q,"title":"Questions",
"metadata":{"kind":"question"},
"fields":[{"key":"q0","title":"Color Preference","description":"Do you prefer red or blue?",
"type":"string","options":[{"value":"Red","label":"Red"},{"value":"Blue","label":"Blue"}],
"custom":true}]}`, spikeForm, spikeSession))
}

func formCreatedEvent() string {
	return fmt.Sprintf(`{"id":"evt_f","type":"form.created","data":{"form":%s}}`, spikeFormJSON())
}

func formRepliedEvent(answer string) string {
	return fmt.Sprintf(`{"id":"e3","type":"form.replied",
"data":{"id":%q,"sessionID":%q,"answer":{"q0":%q}}}`, spikeForm, spikeSession, answer)
}

func newMirror(t *testing.T, f *fakeService, rec *fakeRecorder, mod func(*Config)) *Mirror {
	t.Helper()
	cfg := Config{
		Host: "eve", StateFile: f.stateFile, Recorder: rec, Backoff: 10 * time.Millisecond,
	}
	if mod != nil {
		mod(&cfg)
	}
	return New(cfg)
}

func run(t *testing.T, m *Mirror) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = m.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
}

func spikeService(t *testing.T) *fakeService {
	f := newFakeService(t)
	f.addSession(spikeSession, "cockpit spike", "/work/spike")
	f.setMessages(spikeSession, spikeMessages(t)...)
	return f
}

// runSynced starts the mirror and waits until history is mirrored and the live stream is open.
func runSynced(t *testing.T, f *fakeService, rec *fakeRecorder, m *Mirror) {
	t.Helper()
	run(t, m)
	rec.waitType(t, agents.TypeTurnEnded, 1)
	waitFor(t, "stream", func() bool { return f.streamCount() == 1 })
}

// ---- tests ----

func TestDiscover(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	good := fmt.Sprintf(
		`{"id":"x","version":"2.2.6","url":"http://127.0.0.1:1234","pid":%d,"password":"s3"}`,
		os.Getpid())
	ep, err := Discover(write("good.json", good))
	if err != nil {
		t.Fatal(err)
	}
	if ep.URL != "http://127.0.0.1:1234" || ep.Password != "s3" || ep.Version != "2.2.6" {
		t.Fatalf("endpoint = %+v", ep)
	}

	if _, err := Discover(filepath.Join(dir, "missing.json")); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("missing file: %v, want ErrNotRunning", err)
	}
	if err := syscall.Kill(2147483646, 0); err == nil {
		t.Skip("pid unexpectedly alive")
	}
	dead := `{"url":"http://127.0.0.1:1","pid":2147483646,"password":"s3"}`
	if _, err := Discover(write("dead.json", dead)); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("dead pid: %v, want ErrNotRunning", err)
	}
	if _, err := Discover(write("bad.json", "{")); err == nil || errors.Is(err, ErrNotRunning) {
		t.Fatalf("corrupt file: %v, want a parse error", err)
	}
}

func TestCatchUpMapsHistory(t *testing.T) {
	f, rec := spikeService(t), &fakeRecorder{}
	run(t, newMirror(t, f, rec, nil))

	want := []agents.EventType{
		agents.TypeSessionCreated,
		agents.TypeMessageUser, agents.TypeTurnStarted, agents.TypeSessionStatus,
		agents.TypeToolCall, agents.TypeToolUpdate,
		agents.TypeMessageAssistant,
		agents.TypeTurnEnded, agents.TypeSessionStatus,
	}
	waitFor(t, "full history", func() bool { return len(rec.snapshot()) >= len(want) })
	got := rec.types()[:len(want)]
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event %d = %s, want %s\nall: %v", i, got[i], want[i], got)
		}
	}
	evs := rec.snapshot()
	for _, e := range evs {
		if e.Session != spikeSession {
			t.Fatalf("session = %q, want the native id", e.Session)
		}
	}
	created := evs[0].Payload.(agents.SessionCreated)
	if created.Agent != "opencode" || created.Dir != "/work/spike" ||
		created.NativeID != spikeSession || created.Title != "cockpit spike" ||
		created.Model != "github-copilot/gemini-3.8-flash" {
		t.Fatalf("session.created = %+v", created)
	}
	if u := evs[1].Payload.(agents.MessageUser); u.MessageID != spikeUserMsg ||
		!strings.HasPrefix(u.Text, "Use your question tool") {
		t.Fatalf("message.user = %+v", u)
	}
	if ts := evs[2].Payload.(agents.TurnStarted); ts.TurnID != spikeUserMsg {
		t.Fatalf("turn.started = %+v", ts)
	}
	if st := evs[3].Payload.(agents.SessionStatus); st.State != agents.StateRunning {
		t.Fatalf("status after turn.started = %+v", st)
	}
	if tc := evs[4].Payload.(agents.ToolCall); tc.Name != "question" || tc.TurnID != spikeUserMsg {
		t.Fatalf("tool.call = %+v", tc)
	}
	if tu := evs[5].Payload.(agents.ToolUpdate); tu.Status != "completed" {
		t.Fatalf("tool.update = %+v", tu)
	}
	if a := evs[6].Payload.(agents.MessageAssistant); a.Text != "You prefer red." {
		t.Fatalf("message.assistant = %+v", a)
	}
	te := evs[7].Payload.(agents.TurnEnded)
	if te.TurnID != spikeUserMsg || te.StopReason != agents.StopEndTurn ||
		te.ToolsSummary.Count != 1 {
		t.Fatalf("turn.ended = %+v", te)
	}
	if st := evs[8].Payload.(agents.SessionStatus); st.State != agents.StateIdle {
		t.Fatalf("final status = %+v", st)
	}
	if n := f.logCallCount(); n != 0 {
		t.Fatalf("/log endpoint called %d times; it does not replay in 2.2.6", n)
	}
}

func TestCatchUpIsIdempotentAcrossRestarts(t *testing.T) {
	f, rec := spikeService(t), &fakeRecorder{}
	seen := map[string]map[string]bool{spikeSession: {}}
	for _, raw := range spikeMessages(t) {
		var m struct{ ID string }
		_ = json.Unmarshal(raw, &m)
		seen[spikeSession][m.ID] = true
	}
	run(t, newMirror(t, f, rec, func(c *Config) { c.Seen = seen }))
	waitFor(t, "stream", func() bool { return f.streamCount() == 1 })
	time.Sleep(50 * time.Millisecond)
	if got := rec.types(); len(got) != 0 {
		t.Fatalf("recorded %v for already-seen history", got)
	}
}

func TestLiveTurnIsMirroredOnExecutionEvents(t *testing.T) {
	f, rec := spikeService(t), &fakeRecorder{}
	runSynced(t, f, rec, newMirror(t, f, rec, nil))

	f.appendMessages(spikeSession,
		json.RawMessage(`{"id":"msg_u2","type":"user","time":{"created":2},"text":"hi from the TUI"}`),
		json.RawMessage(`{"id":"msg_a2","type":"assistant","time":{"created":3,"completed":3},
"content":[{"type":"text","text":"hello"}],"finish":"stop"}`),
		json.RawMessage(`{"id":"msg_i2","type":"idle","time":{"created":4},"outcome":"succeeded"}`),
	)
	f.push(fmt.Sprintf(`{"id":"e1","type":"session.execution.succeeded",
"data":{"sessionID":%q},"durable":{"aggregateID":%q,"seq":9,"version":1}}`,
		spikeSession, spikeSession))

	rec.waitType(t, agents.TypeTurnEnded, 2)
	users := rec.of(agents.TypeMessageUser)
	if len(users) != 2 || users[1].Payload.(agents.MessageUser).Text != "hi from the TUI" {
		t.Fatalf("users = %+v", users)
	}
}

func TestNewSessionOnStreamIsRegistered(t *testing.T) {
	f, rec := spikeService(t), &fakeRecorder{}
	runSynced(t, f, rec, newMirror(t, f, rec, nil))

	f.addSession("ses_new", "fresh", "/work/new")
	f.push(`{"id":"e2","type":"session.created","data":{"sessionID":"ses_new"},
"durable":{"aggregateID":"ses_new","seq":0,"version":1}}`)
	rec.waitType(t, agents.TypeSessionCreated, 2)
	c := rec.of(agents.TypeSessionCreated)[1]
	if c.Session != "ses_new" || c.Payload.(agents.SessionCreated).Dir != "/work/new" {
		t.Fatalf("created = %+v", c)
	}
}

func TestFormBecomesAttentionAndReplyResolvesOnce(t *testing.T) {
	f, rec := spikeService(t), &fakeRecorder{}
	m := newMirror(t, f, rec, nil)
	runSynced(t, f, rec, m)

	f.setForms(spikeFormJSON())
	f.push(formCreatedEvent())
	rec.waitType(t, agents.TypeAttentionRequested, 1)
	req := rec.of(agents.TypeAttentionRequested)[0]
	a := req.Payload.(agents.AttentionRequested)
	if req.Session != spikeSession || a.AttentionID != spikeForm ||
		a.Kind != agents.AttentionQuestion || !a.AllowText ||
		a.Title != "Do you prefer red or blue?" ||
		len(a.Options) != 2 || a.Options[0].Label != "Red" || a.Options[0].ID != "Red" ||
		a.Native == nil || a.Native.FormID != spikeForm {
		t.Fatalf("attention = %+v", a)
	}

	if err := m.Reply(context.Background(), spikeForm, "Red", "discord"); err != nil {
		t.Fatal(err)
	}
	replies := f.repliesSent()
	if len(replies) != 1 || !strings.HasPrefix(replies[0], spikeForm+" ") ||
		!strings.Contains(replies[0], `"answer":{"q0":"Red"}`) {
		t.Fatalf("replies = %q", replies)
	}
	f.push(formRepliedEvent("Red"))
	time.Sleep(50 * time.Millisecond)
	res := rec.of(agents.TypeAttentionResolved)
	if len(res) != 1 {
		t.Fatalf("resolved %d times, want exactly once: %+v", len(res), res)
	}
	if r := res[0].Payload.(agents.AttentionResolved); r.By != "discord" || r.Answer != "Red" {
		t.Fatalf("resolved = %+v", r)
	}
}

func TestFormRepliedElsewhereIsRecordedAsExternal(t *testing.T) {
	f, rec := spikeService(t), &fakeRecorder{}
	runSynced(t, f, rec, newMirror(t, f, rec, nil))
	f.push(formCreatedEvent())
	rec.waitType(t, agents.TypeAttentionRequested, 1)
	f.push(formRepliedEvent("Blue"))
	rec.waitType(t, agents.TypeAttentionResolved, 1)
	r := rec.of(agents.TypeAttentionResolved)[0].Payload.(agents.AttentionResolved)
	if r.By != "opencode" || r.Answer != "Blue" {
		t.Fatalf("resolved = %+v", r)
	}
}

func TestReplyLosingTheRaceReturnsErrResolved(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusConflict} {
		f, rec := spikeService(t), &fakeRecorder{}
		m := newMirror(t, f, rec, nil)
		runSynced(t, f, rec, m)
		f.push(formCreatedEvent())
		rec.waitType(t, agents.TypeAttentionRequested, 1)

		f.setReplyErr(code)
		err := m.Reply(context.Background(), spikeForm, "Red", "discord")
		if !errors.Is(err, ErrResolved) {
			t.Fatalf("status %d: err = %v, want ErrResolved", code, err)
		}
		if n := len(rec.of(agents.TypeAttentionResolved)); n != 0 {
			t.Fatalf("status %d: recorded a resolution for a lost race", code)
		}
	}
	m := newMirror(t, spikeService(t), &fakeRecorder{}, nil)
	if err := m.Reply(context.Background(), "frm_unknown", "x", "discord"); err == nil {
		t.Fatal("reply to an unknown attention succeeded")
	}
}

func TestSendPostsPrompt(t *testing.T) {
	f, rec := spikeService(t), &fakeRecorder{}
	m := newMirror(t, f, rec, nil)
	if err := m.Send(context.Background(), spikeSession, "do the thing"); err != nil {
		t.Fatal(err)
	}
	if got := f.promptsSent(); len(got) != 1 || got[0] != "do the thing" {
		t.Fatalf("prompts = %q", got)
	}
	if err := m.Send(context.Background(), "ses_missing", "x"); err == nil {
		t.Fatal("send to a missing session succeeded")
	}
}

func TestServiceRestartCancelsVanishedFormsAndRelearnsCredentials(t *testing.T) {
	f, rec := newFakeService(t), &fakeRecorder{}
	f.addSession(spikeSession, "cockpit spike", "/work/spike")
	f.setMessages(spikeSession, json.RawMessage(
		`{"id":"msg_u1","type":"user","time":{"created":1},"text":"ask me"}`))
	f.setActive(spikeSession, true)
	run(t, newMirror(t, f, rec, nil))
	rec.waitType(t, agents.TypeTurnStarted, 1)
	waitFor(t, "stream", func() bool { return f.streamCount() == 1 })

	f.setForms(spikeFormJSON())
	f.push(formCreatedEvent())
	rec.waitType(t, agents.TypeAttentionRequested, 1)

	f.restart()

	rec.waitType(t, agents.TypeAttentionResolved, 1)
	r := rec.of(agents.TypeAttentionResolved)[0].Payload.(agents.AttentionResolved)
	if r.AttentionID != spikeForm || r.By != "cancelled" {
		t.Fatalf("resolved = %+v", r)
	}
	rec.waitType(t, agents.TypeTurnEnded, 1)
	te := rec.of(agents.TypeTurnEnded)[0].Payload.(agents.TurnEnded)
	if te.TurnID != "msg_u1" || te.StopReason != agents.StopInterrupted {
		t.Fatalf("turn.ended = %+v", te)
	}
	waitFor(t, "reconnect with new credentials", func() bool { return f.streamCount() == 1 })
	if n := len(rec.of(agents.TypeMessageUser)); n != 1 {
		t.Fatalf("history re-recorded after restart: %d user messages", n)
	}
	if n := len(rec.of(agents.TypeTurnEnded)); n != 1 {
		t.Fatalf("turn ended %d times", n)
	}
}

func TestPendingFormsAtStartupBecomeAttention(t *testing.T) {
	f, rec := spikeService(t), &fakeRecorder{}
	f.setForms(spikeFormJSON())
	run(t, newMirror(t, f, rec, nil))
	rec.waitType(t, agents.TypeAttentionRequested, 1)
	time.Sleep(50 * time.Millisecond)
	if n := len(rec.of(agents.TypeAttentionRequested)); n != 1 {
		t.Fatalf("attention requested %d times", n)
	}
}

func TestStartsServiceWhenNotRunning(t *testing.T) {
	f, rec := spikeService(t), &fakeRecorder{}
	if err := os.Remove(f.stateFile); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	started := 0
	run(t, newMirror(t, f, rec, func(c *Config) {
		c.Start = func(context.Context) error {
			mu.Lock()
			started++
			mu.Unlock()
			f.writeState()
			return nil
		}
	}))
	rec.waitType(t, agents.TypeTurnEnded, 1)
	mu.Lock()
	defer mu.Unlock()
	if started != 1 {
		t.Fatalf("Start called %d times, want 1", started)
	}
}

func TestTextDeltasAreForwardedLive(t *testing.T) {
	f, rec := spikeService(t), &fakeRecorder{}
	runSynced(t, f, rec, newMirror(t, f, rec, nil))
	f.push(fmt.Sprintf(`{"id":"e4","type":"session.text.delta","data":{"sessionID":%q,
"assistantMessageID":"msg_a9","ordinal":0,"delta":"par"}}`, spikeSession))
	rec.waitType(t, agents.TypeMessageDelta, 1)
	d := rec.of(agents.TypeMessageDelta)[0].Payload.(agents.MessageDelta)
	if d.MessageID != "msg_a9" || d.Text != "par" {
		t.Fatalf("delta = %+v", d)
	}
}

type memStore struct {
	mu   sync.Mutex
	seen map[string]map[string]bool
}

func (s *memStore) LoadSeen(context.Context) (map[string]map[string]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]map[string]bool{}
	for k, v := range s.seen {
		out[k] = map[string]bool{}
		for id := range v {
			out[k][id] = true
		}
	}
	return out, nil
}

func (s *memStore) MarkSeen(_ context.Context, session, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen == nil {
		s.seen = map[string]map[string]bool{}
	}
	if s.seen[session] == nil {
		s.seen[session] = map[string]bool{}
	}
	s.seen[session][id] = true
	return nil
}

func TestStoreMakesCatchUpIdempotentAcrossClaraRestarts(t *testing.T) {
	f, st := spikeService(t), &memStore{}
	rec1 := &fakeRecorder{}
	ctx1, cancel1 := context.WithCancel(context.Background())
	done := make(chan struct{})
	m1 := newMirror(t, f, rec1, func(c *Config) { c.Store = st })
	go func() { defer close(done); _ = m1.Run(ctx1) }()
	rec1.waitType(t, agents.TypeTurnEnded, 1)
	waitFor(t, "stream", func() bool { return f.streamCount() >= 1 })
	cancel1()
	<-done

	rec2 := &fakeRecorder{}
	run(t, newMirror(t, f, rec2, func(c *Config) { c.Store = st }))
	waitFor(t, "second stream", func() bool { return f.streamCount() >= 2 })
	time.Sleep(50 * time.Millisecond)
	if got := rec2.types(); len(got) != 0 {
		t.Fatalf("restarted mirror re-recorded %v", got)
	}
}

func TestSQLStoreRoundTrip(t *testing.T) {
	db, err := store.OpenMemory(zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := NewSQLStore(db.DB())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, id := range []string{"", "m1", "m1", "m2"} {
		if err := s.MarkSeen(ctx, "ses_a", id); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.LoadSeen(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got["ses_a"]) != 3 || !got["ses_a"]["m2"] || !got["ses_a"][""] {
		t.Fatalf("seen = %v", got)
	}
}

func TestInterruptPostsToSession(t *testing.T) {
	f := spikeService(t)
	m := newMirror(t, f, &fakeRecorder{}, nil)
	if err := m.Interrupt(context.Background(), spikeSession); err != nil {
		t.Fatal(err)
	}
	if got := f.interruptsSent(); len(got) != 1 || got[0] != spikeSession {
		t.Fatalf("interrupts = %q", got)
	}
}
