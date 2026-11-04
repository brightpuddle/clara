package adapter

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/internal/agents"
)

// Dialer starts an agent and returns its stdio as one stream. dir is the
// working directory for the agent, empty when not session-specific.
type Dialer func(ctx context.Context, dir string) (io.ReadWriteCloser, error)

// ACP is an Adapter for any agent speaking the Agent Client Protocol.
type ACP struct {
	name   string
	dial   Dialer
	quirks Quirks
	log    zerolog.Logger
}

func NewACP(name string, dial Dialer, quirks Quirks, log zerolog.Logger) *ACP {
	return &ACP{name: name, dial: dial, quirks: quirks, log: log}
}

// NewAgy returns the adapter for `agy acp`.
func NewAgy(bin string, log zerolog.Logger) *ACP {
	return NewACP("agy", ExecDialer(bin, []string{"acp"}, log), AgyQuirks, log)
}

// NewOpencode returns the adapter for `opencode acp`.
func NewOpencode(bin string, log zerolog.Logger) *ACP {
	return NewACP("opencode", ExecDialer(bin, []string{"acp"}, log), OpencodeQuirks, log)
}

func (a *ACP) Name() string { return a.name }

func (a *ACP) handshake(ctx context.Context, c *rpcConn) error {
	err := c.Call(ctx, "initialize", map[string]any{
		"protocolVersion":    1,
		"clientCapabilities": map[string]any{},
	}, nil)
	return errors.Wrapf(err, "%s: initialize", a.name)
}

// List starts a short-lived agent process and returns its resumable sessions.
func (a *ACP) List(ctx context.Context) ([]NativeSession, error) {
	rw, err := a.dial(ctx, "")
	if err != nil {
		return nil, errors.Wrapf(err, "%s: start agent", a.name)
	}
	c := newRPCConn(rw, nil, nil, func(error) { _ = rw.Close() })
	defer func() {
		_ = c.Close()
		<-c.Done()
	}()
	if err := a.handshake(ctx, c); err != nil {
		return nil, err
	}

	var out []NativeSession
	cursor := ""
	for {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var res struct {
			Sessions []struct {
				SessionID string `json:"sessionId"`
				Title     string `json:"title"`
				Cwd       string `json:"cwd"`
				UpdatedAt string `json:"updatedAt"`
			} `json:"sessions"`
			NextCursor string `json:"nextCursor"`
		}
		if err := c.Call(ctx, "session/list", params, &res); err != nil {
			return nil, errors.Wrapf(err, "%s: list sessions", a.name)
		}
		for _, s := range res.Sessions {
			ns := NativeSession{ID: s.SessionID, Title: s.Title, Dir: s.Cwd}
			if t, err := time.Parse(time.RFC3339Nano, s.UpdatedAt); err == nil {
				ns.UpdatedAt = t
			}
			out = append(out, ns)
		}
		if res.NextCursor == "" || res.NextCursor == cursor {
			return out, nil
		}
		cursor = res.NextCursor
	}
}

// Open starts an agent process and opens or resumes one session on it. ctx
// bounds the session: cancelling it closes the session and process.
func (a *ACP) Open(ctx context.Context, spec OpenSpec) (AgentSession, error) {
	if spec.Dir == "" {
		return nil, errors.Newf("%s: open: dir is required", a.name)
	}
	rw, err := a.dial(ctx, spec.Dir)
	if err != nil {
		return nil, errors.Wrapf(err, "%s: start agent", a.name)
	}
	s := &acpSession{
		a:         a,
		rw:        rw,
		events:    make(chan AgentEvent, 256),
		abandoned: make(chan struct{}),
	}
	s.conn = newRPCConn(rw, s.handleRequest, s.handleNotify, s.onClose)

	if err := s.start(ctx, spec); err != nil {
		close(s.abandoned)
		s.closeRequested.Store(true)
		_ = s.conn.Close()
		<-s.conn.Done()
		return nil, err
	}
	go func() {
		select {
		case <-ctx.Done():
			_ = s.Close(context.Background())
		case <-s.conn.Done():
		}
	}()
	return s, nil
}

type acpSession struct {
	a      *ACP
	rw     io.ReadWriteCloser
	conn   *rpcConn
	events chan AgentEvent

	abandoned      chan struct{} // closed when nobody will read events (failed open)
	closeRequested atomic.Bool

	mu       sync.Mutex
	nativeID string
	live     bool   // false while session/load replays history
	inflight string // prompt id awaiting its response; empty when idle

	// Only touched on the read goroutine.
	msgID  string
	msgBuf strings.Builder
}

func (s *acpSession) NativeID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nativeID
}

func (s *acpSession) Events() <-chan AgentEvent { return s.events }

func (s *acpSession) start(ctx context.Context, spec OpenSpec) error {
	if err := s.a.handshake(ctx, s.conn); err != nil {
		return err
	}
	if spec.ResumeID != "" {
		s.mu.Lock()
		s.nativeID = spec.ResumeID
		s.mu.Unlock()
		err := s.conn.call(ctx, "session/load", map[string]any{
			"sessionId":  spec.ResumeID,
			"cwd":        spec.Dir,
			"mcpServers": []any{},
		}, nil, func(json.RawMessage) {
			s.mu.Lock()
			s.live = true
			s.mu.Unlock()
		})
		if err != nil {
			return errors.Wrapf(err, "%s: load session", s.a.name)
		}
	} else {
		err := s.conn.call(ctx, "session/new", map[string]any{
			"cwd":        spec.Dir,
			"mcpServers": []any{},
		}, nil, func(res json.RawMessage) {
			var r struct {
				SessionID string `json:"sessionId"`
			}
			_ = json.Unmarshal(res, &r)
			s.mu.Lock()
			s.nativeID = r.SessionID
			s.live = r.SessionID != ""
			s.mu.Unlock()
		})
		if err != nil {
			return errors.Wrapf(err, "%s: new session", s.a.name)
		}
		if s.NativeID() == "" {
			return errors.Newf("%s: new session returned no session id", s.a.name)
		}
	}
	if spec.Model != "" {
		if s.a.quirks.SetModel == nil {
			return errors.Newf("%s: model selection is not supported", s.a.name)
		}
		if err := s.a.quirks.SetModel(ctx, s.conn, s.NativeID(), spec.Model); err != nil {
			return errors.Wrapf(err, "%s: set model %q", s.a.name, spec.Model)
		}
	}
	return nil
}

func (s *acpSession) Prompt(_ context.Context, p Prompt) error {
	s.mu.Lock()
	if s.inflight != "" {
		s.mu.Unlock()
		return ErrBusy
	}
	s.inflight = p.ID
	id := s.nativeID
	s.mu.Unlock()

	err := s.conn.Go("session/prompt", map[string]any{
		"sessionId": id,
		"prompt":    []any{map[string]any{"type": "text", "text": p.Text}},
	}, func(res json.RawMessage, err error) { s.promptReply(p.ID, res, err) })
	if err != nil {
		s.mu.Lock()
		s.inflight = ""
		s.mu.Unlock()
		return errors.Wrap(err, "send prompt")
	}
	return nil
}

// promptReply runs on the read goroutine, so the TurnEnded it emits is ordered
// after every update the agent sent for the turn.
func (s *acpSession) promptReply(promptID string, res json.RawMessage, err error) {
	s.mu.Lock()
	s.inflight = ""
	s.mu.Unlock()
	if errors.Is(err, ErrClosed) {
		return // Exited reports it; the runner marks the open turn interrupted
	}
	s.flush()

	ev := TurnEnded{PromptID: promptID}
	if err != nil {
		ev.StopReason = agents.StopError
		ev.Err = err.Error()
		s.emit(ev)
		return
	}
	var r struct {
		StopReason string `json:"stopReason"`
		Usage      *struct {
			InputTokens  int64 `json:"inputTokens"`
			OutputTokens int64 `json:"outputTokens"`
			TotalTokens  int64 `json:"totalTokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		ev.StopReason = agents.StopError
		ev.Err = "decode prompt result: " + err.Error()
		s.emit(ev)
		return
	}
	switch r.StopReason {
	case "end_turn":
		ev.StopReason = agents.StopEndTurn
	case "cancelled":
		ev.StopReason = agents.StopCancelled
	default:
		ev.StopReason = agents.StopError
		ev.Err = "stop reason: " + r.StopReason
	}
	if r.Usage != nil {
		ev.Usage = &agents.Usage{
			InputTokens:  r.Usage.InputTokens,
			OutputTokens: r.Usage.OutputTokens,
			TotalTokens:  r.Usage.TotalTokens,
		}
	}
	s.emit(ev)
}

func (s *acpSession) Cancel(_ context.Context) error {
	s.mu.Lock()
	busy := s.inflight != ""
	id := s.nativeID
	s.mu.Unlock()
	if !busy {
		return nil
	}
	return s.conn.Notify("session/cancel", map[string]any{"sessionId": id})
}

func (s *acpSession) Close(ctx context.Context) error {
	s.closeRequested.Store(true)
	err := s.conn.Close()
	select {
	case <-s.conn.Done():
	case <-ctx.Done():
		return ctx.Err()
	}
	return err
}

func (s *acpSession) emit(ev AgentEvent) {
	select {
	case s.events <- ev:
	case <-s.abandoned:
	}
}

// onClose runs on the read goroutine after the connection ends.
func (s *acpSession) onClose(loopErr error) {
	s.flush()
	err := loopErr
	if err == nil && !s.closeRequested.Load() {
		err = errors.New("agent process exited")
	}
	s.emit(Exited{Err: err})
	close(s.events)
	_ = s.rw.Close()
}

func (s *acpSession) handleRequest(method string, params json.RawMessage) (any, error) {
	switch method {
	case "session/request_permission":
		// Agents run fully permissive; approve the first allowing option.
		var p struct {
			Options []struct {
				OptionID string `json:"optionId"`
				Kind     string `json:"kind"`
			} `json:"options"`
		}
		_ = json.Unmarshal(params, &p)
		for _, o := range p.Options {
			if strings.HasPrefix(o.Kind, "allow") {
				return map[string]any{
					"outcome": map[string]any{"outcome": "selected", "optionId": o.OptionID},
				}, nil
			}
		}
		return map[string]any{"outcome": map[string]any{"outcome": "cancelled"}}, nil
	}
	return nil, &RPCError{Code: codeMethodNotFound, Message: "method not found: " + method}
}

type sessionUpdate struct {
	SessionUpdate string          `json:"sessionUpdate"`
	MessageID     string          `json:"messageId"`
	Title         string          `json:"title"`
	Content       json.RawMessage `json:"content"` // object for messages, array for tools

	ToolCallID string `json:"toolCallId"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	Locations  []struct {
		Path string `json:"path"`
	} `json:"locations"`

	Used int64 `json:"used"`
	Size int64 `json:"size"`
	Cost *struct {
		Amount float64 `json:"amount"`
	} `json:"cost"`
}

func (s *acpSession) handleNotify(method string, params json.RawMessage) {
	if method != "session/update" {
		return
	}
	var p struct {
		SessionID string          `json:"sessionId"`
		Update    json.RawMessage `json:"update"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	s.mu.Lock()
	ours := s.live && p.SessionID == s.nativeID
	s.mu.Unlock()
	// Not live: history replayed by session/load. Other ids: child sessions.
	if !ours {
		return
	}
	var u sessionUpdate
	if json.Unmarshal(p.Update, &u) != nil {
		return
	}

	switch u.SessionUpdate {
	case "agent_message_chunk":
		var c struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(u.Content, &c) != nil || c.Type != "text" || c.Text == "" {
			return
		}
		if s.msgBuf.Len() > 0 && u.MessageID != "" && u.MessageID != s.msgID {
			s.flush()
		}
		if s.msgBuf.Len() == 0 {
			s.msgID = u.MessageID
			if s.msgID == "" {
				s.msgID = agents.NewID()
			}
		}
		s.msgBuf.WriteString(c.Text)
		s.emit(MessageDelta{MessageID: s.msgID, Text: c.Text})
	case "tool_call":
		s.flush()
		tc := ToolCall{
			CallID: u.ToolCallID, Name: u.Name, Kind: u.Kind, Title: u.Title, Status: u.Status,
		}
		for _, l := range u.Locations {
			tc.Locations = append(tc.Locations, l.Path)
		}
		s.emit(tc)
	case "tool_call_update":
		s.flush()
		tu := ToolUpdate{CallID: u.ToolCallID, Status: u.Status, Title: u.Title}
		var parts []struct {
			Content struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		if json.Unmarshal(u.Content, &parts) == nil && len(parts) > 0 {
			tu.OutputSummary = summarize(parts[0].Content.Text)
		}
		s.emit(tu)
	case "session_info_update":
		if u.Title != "" {
			s.emit(TitleUpdate{Title: u.Title})
		}
	case "usage_update":
		uu := UsageUpdate{Used: u.Used, Size: u.Size}
		if u.Cost != nil {
			c := u.Cost.Amount
			uu.Cost = &c
		}
		s.emit(uu)
	}
}

// flush ends the assistant message being streamed, if any.
func (s *acpSession) flush() {
	if s.msgBuf.Len() == 0 {
		return
	}
	text := s.msgBuf.String()
	id := s.msgID
	s.msgBuf.Reset()
	s.msgID = ""
	s.emit(MessageDone{MessageID: id, Text: text})
	if s.a.quirks.DetectBlocked != nil {
		if b := s.a.quirks.DetectBlocked(text); b != nil {
			s.emit(Blocked{*b})
		}
	}
}

func summarize(text string) string {
	const max = 200
	text = strings.TrimSpace(text)
	if len(text) <= max {
		return text
	}
	return text[:max] + "…"
}

// ExecDialer runs `bin args...` as the agent process.
func ExecDialer(bin string, args []string, log zerolog.Logger) Dialer {
	return func(_ context.Context, dir string) (io.ReadWriteCloser, error) {
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		cmd.Env = managedEnv(os.Environ())
		cmd.Stderr = stderrLog{log.With().Str("agent", bin).Logger()}
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, errors.Wrap(err, "stdin pipe")
		}
		// An os.Pipe keeps the read side ours, so Wait never closes it under the reader.
		pr, pw, err := os.Pipe()
		if err != nil {
			return nil, errors.Wrap(err, "stdout pipe")
		}
		cmd.Stdout = pw
		if err := cmd.Start(); err != nil {
			_ = pr.Close()
			_ = pw.Close()
			return nil, errors.Wrapf(err, "start %s", bin)
		}
		_ = pw.Close()
		p := &procConn{cmd: cmd, stdin: stdin, stdout: pr, exited: make(chan struct{})}
		go func() {
			_ = cmd.Wait()
			close(p.exited)
		}()
		return p, nil
	}
}

// managedEnv hides tmux from a managed agent and marks it as managed, so
// the legacy tmux-based agy chat hooks never mirror its turns a second time.
func managedEnv(env []string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if strings.HasPrefix(kv, "TMUX=") || strings.HasPrefix(kv, "TMUX_PANE=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "CLARA_MANAGED_AGENT=1")
}

type stderrLog struct{ l zerolog.Logger }

func (w stderrLog) Write(b []byte) (int, error) {
	if msg := strings.TrimSpace(string(b)); msg != "" {
		w.l.Debug().Msg(msg)
	}
	return len(b), nil
}

type procConn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *os.File
	exited chan struct{}
	once   sync.Once
}

func (p *procConn) Read(b []byte) (int, error)  { return p.stdout.Read(b) }
func (p *procConn) Write(b []byte) (int, error) { return p.stdin.Write(b) }

// Close asks the agent to exit by closing its stdin, then kills it after a grace period.
func (p *procConn) Close() error {
	p.once.Do(func() {
		_ = p.stdin.Close()
		select {
		case <-p.exited:
		case <-time.After(2 * time.Second):
			_ = p.cmd.Process.Kill()
			<-p.exited
		}
		_ = p.stdout.Close()
	})
	return nil
}
