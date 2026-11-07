// Package agent exposes the managed agent core as MCP tools under the "agent"
// namespace and mirrors every logged agent event onto the event bus as a
// CloudEvent of type "agent.<event type>".
package agent

import (
	"context"
	"encoding/json"

	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/internal/agents"
	"github.com/brightpuddle/clara/internal/agents/adapter"
	"github.com/brightpuddle/clara/internal/agents/manager"
	"github.com/brightpuddle/clara/internal/config"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/supervisor"
)

const description = "Managed coding-agent sessions (agy, opencode) driven over ACP."

// BuildAdapters creates the ACP adapters named in cfg.
func BuildAdapters(cfg config.AgentsConfig, log zerolog.Logger) map[string]adapter.Adapter {
	out := map[string]adapter.Adapter{}
	for name, c := range cfg.AdapterConfigs() {
		bin := c.Bin
		if bin == "" {
			bin = name
		}
		switch name {
		case "agy":
			out[name] = adapter.NewAgy(bin, log)
		case "opencode":
			out[name] = adapter.NewOpencode(bin, log)
		default:
			log.Warn().Str("agent", name).Msg("unknown agent adapter; ignored")
		}
	}
	return out
}

// PublishToBus mirrors each logged event onto bus as a CloudEvent.
func PublishToBus(mgr *manager.Manager, bus *supervisor.EventBus) {
	mgr.Subscribe(func(ev agents.Event) { bus.PublishCloud(CloudEvent(ev)) })
}

// CloudEvent converts a host event into a CloudEvent.
func CloudEvent(ev agents.Event) supervisor.CloudEvent {
	var payload any
	_ = json.Unmarshal(ev.Payload, &payload)
	return supervisor.CloudEvent{
		ID:     ev.ID,
		Source: "clara/agents/" + ev.Host,
		Type:   "agent." + string(ev.Type),
		Time:   ev.TS,
		Data: map[string]any{
			"host":     ev.Host,
			"session":  ev.Session,
			"host_seq": ev.HostSeq,
			"payload":  payload,
		},
		ContentType: "application/json",
	}
}

// Register adds the agent.* tools to reg. A nil mgr registers the tool specs
// only (used for type generation); calls then fail with a clear error.
func Register(
	_ context.Context, mgr *manager.Manager, reg *registry.Registry, log zerolog.Logger,
) error {
	reg.RegisterNamespaceDescription("agent", description)
	s := &service{mgr: mgr}

	str := func(desc string) []mcp.PropertyOption {
		return []mcp.PropertyOption{mcp.Description(desc)}
	}
	req := func(desc string) []mcp.PropertyOption {
		return []mcp.PropertyOption{mcp.Required(), mcp.Description(desc)}
	}
	add := func(name, desc string, h registry.Tool, opts ...mcp.ToolOption) {
		reg.RegisterWithSpec(
			mcp.NewTool(name, append([]mcp.ToolOption{mcp.WithDescription(desc)}, opts...)...), h)
	}

	add("agent.session.create", "Start a new managed agent session; returns its id.", s.create,
		mcp.WithString("agent", req("Agent type: agy or opencode.")...),
		mcp.WithString("dir", req("Working directory.")...),
		mcp.WithString("model", str("Model override.")...),
		mcp.WithString("kind", str("longlived (default) or task.")...),
		mcp.WithString("prompt", str("Optional first prompt.")...),
		mcp.WithString("title", str("Display name; the agent may replace it later.")...),
	)
	add("agent.session.list", "List sessions with their state.", s.list)
	add("agent.session.get", "Get one session with its queued prompts.", s.get,
		mcp.WithString("session", req("Session id.")...))
	add("agent.session.close", "Stop a session and its agent process.", s.close,
		mcp.WithString("session", req("Session id.")...))
	add("agent.session.resume", "Adopt an existing native agent conversation.", s.resume,
		mcp.WithString("agent", req("Agent type.")...),
		mcp.WithString("native_id", req("Native session id from agent.session.discover.")...))
	add("agent.session.discover", "List resumable native sessions.", s.discover,
		mcp.WithString("agent", str("Agent type; all agents if omitted.")...))
	add("agent.session.model", "Switch the model of an idle session (agent-specific model id).",
		s.setModel,
		mcp.WithString("session", req("Session id.")...),
		mcp.WithString("model", req("Model id, e.g. gemini-3.8-flash-high.")...))
	add("agent.session.clear", "Completely reinitialize the session with a fresh context.", s.clear,
		mcp.WithString("session", req("Session id.")...))
	add("agent.prompt.send", "Queue a prompt; it runs when the session is idle.", s.send,
		mcp.WithString("session", req("Session id.")...),
		mcp.WithString("text", req("Prompt text.")...),
		mcp.WithString("origin", str("Who sent it (for display), e.g. discord.")...))
	add("agent.prompt.cancel", "Remove a queued prompt that has not been sent.", s.cancelPrompt,
		mcp.WithString("session", req("Session id.")...),
		mcp.WithString("prompt_id", req("Prompt id.")...))
	add("agent.turn.interrupt", "Cancel the running turn; queued prompts continue.", s.interrupt,
		mcp.WithString("session", req("Session id.")...))
	add("agent.events.read", "Read logged events after a host_seq cursor.", s.readEvents,
		mcp.WithString("session", str("Session id; all sessions if omitted.")...),
		mcp.WithNumber("after", mcp.Description("Return events with host_seq greater than this.")),
		mcp.WithNumber("limit", mcp.Description("Maximum events (default and cap 1000).")))

	log.Debug().Msg("registered agent builtin")
	return nil
}

type service struct{ mgr *manager.Manager }

var errDisabled = errors.New("agent core is not enabled (set agents.enabled in config)")

func (s *service) check() error {
	if s.mgr == nil {
		return errDisabled
	}
	return nil
}

func argStr(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

func argInt(args map[string]any, key string) int64 {
	switch v := args[key].(type) {
	case float64:
		return int64(v)
	case int:
		return int64(v)
	case int64:
		return v
	}
	return 0
}

func requireStr(args map[string]any, name, key string) (string, error) {
	v := argStr(args, key)
	if v == "" {
		return "", errors.Newf("%s: %q is required", name, key)
	}
	return v, nil
}

func (s *service) create(ctx context.Context, args map[string]any) (any, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	agent, err := requireStr(args, "agent.session.create", "agent")
	if err != nil {
		return nil, err
	}
	dir, err := requireStr(args, "agent.session.create", "dir")
	if err != nil {
		return nil, err
	}
	id, err := s.mgr.Create(ctx, agents.SessionCreate{
		Host: s.mgr.Host(), Agent: agent, Dir: dir, Model: argStr(args, "model"),
		Kind: agents.SessionKind(argStr(args, "kind")), Prompt: argStr(args, "prompt"),
		Title: argStr(args, "title"),
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"session": id}, nil
}

func (s *service) list(_ context.Context, _ map[string]any) (any, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	return map[string]any{"sessions": s.mgr.List()}, nil
}

func (s *service) get(_ context.Context, args map[string]any) (any, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	id, err := requireStr(args, "agent.session.get", "session")
	if err != nil {
		return nil, err
	}
	sess, ok := s.mgr.Get(id)
	if !ok {
		return nil, errors.Newf("agent.session.get: unknown session %q", id)
	}
	return map[string]any{"session": sess, "queue": s.mgr.Queue(id)}, nil
}

func (s *service) close(ctx context.Context, args map[string]any) (any, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	id, err := requireStr(args, "agent.session.close", "session")
	if err != nil {
		return nil, err
	}
	if err := s.mgr.Close(ctx, id); err != nil {
		return nil, err
	}
	return map[string]any{"session": id, "closed": true}, nil
}

func (s *service) resume(ctx context.Context, args map[string]any) (any, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	agent, err := requireStr(args, "agent.session.resume", "agent")
	if err != nil {
		return nil, err
	}
	native, err := requireStr(args, "agent.session.resume", "native_id")
	if err != nil {
		return nil, err
	}
	id, err := s.mgr.Resume(ctx, agents.SessionResume{
		Host: s.mgr.Host(), Agent: agent, NativeID: native,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"session": id}, nil
}

func (s *service) discover(ctx context.Context, args map[string]any) (any, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	list, err := s.mgr.Discover(ctx, argStr(args, "agent"))
	if err != nil {
		return nil, err
	}
	return map[string]any{"sessions": list}, nil
}

func (s *service) setModel(ctx context.Context, args map[string]any) (any, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	id, err := requireStr(args, "agent.session.model", "session")
	if err != nil {
		return nil, err
	}
	model, err := requireStr(args, "agent.session.model", "model")
	if err != nil {
		return nil, err
	}
	if err := s.mgr.SetModel(ctx, id, model); err != nil {
		return nil, err
	}
	return map[string]any{"session": id, "model": model}, nil
}

func (s *service) clear(ctx context.Context, args map[string]any) (any, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	id, err := requireStr(args, "agent.session.clear", "session")
	if err != nil {
		return nil, err
	}
	if err := s.mgr.Clear(ctx, id); err != nil {
		return nil, err
	}
	return map[string]any{"session": id, "cleared": true}, nil
}

func (s *service) send(_ context.Context, args map[string]any) (any, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	id, err := requireStr(args, "agent.prompt.send", "session")
	if err != nil {
		return nil, err
	}
	text, err := requireStr(args, "agent.prompt.send", "text")
	if err != nil {
		return nil, err
	}
	pid, err := s.mgr.Send(id, text, argStr(args, "origin"))
	if err != nil {
		return nil, err
	}
	return map[string]any{"prompt_id": pid}, nil
}

func (s *service) cancelPrompt(_ context.Context, args map[string]any) (any, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	id, err := requireStr(args, "agent.prompt.cancel", "session")
	if err != nil {
		return nil, err
	}
	pid, err := requireStr(args, "agent.prompt.cancel", "prompt_id")
	if err != nil {
		return nil, err
	}
	if err := s.mgr.CancelPrompt(id, pid); err != nil {
		return nil, err
	}
	return map[string]any{"cancelled": pid}, nil
}

func (s *service) interrupt(ctx context.Context, args map[string]any) (any, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	id, err := requireStr(args, "agent.turn.interrupt", "session")
	if err != nil {
		return nil, err
	}
	if err := s.mgr.Interrupt(ctx, id); err != nil {
		return nil, err
	}
	return map[string]any{"session": id, "interrupted": true}, nil
}

func (s *service) readEvents(ctx context.Context, args map[string]any) (any, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	evs, err := s.mgr.ReadEvents(
		ctx, argStr(args, "session"), argInt(args, "after"), int(argInt(args, "limit")))
	if err != nil {
		return nil, err
	}
	next := argInt(args, "after")
	if n := len(evs); n > 0 {
		next = evs[n-1].HostSeq
	}
	return map[string]any{"events": evs, "next": next}, nil
}
