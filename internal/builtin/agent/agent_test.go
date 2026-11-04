package agent_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/internal/agents"
	"github.com/brightpuddle/clara/internal/agents/adapter"
	"github.com/brightpuddle/clara/internal/agents/manager"
	"github.com/brightpuddle/clara/internal/builtin/agent"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/supervisor"
)

func TestCloudEvent(t *testing.T) {
	ev, err := agents.NewEvent("eve", "s1", agents.TypePromptSent, agents.PromptSent{PromptID: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	ev.HostSeq = 7
	ce := agent.CloudEvent(ev)
	if ce.Type != "agent.prompt.sent" || ce.Source != "clara/agents/eve" {
		t.Fatalf("ce = %+v", ce)
	}
	if ce.Data["session"] != "s1" || ce.Data["host_seq"] != int64(7) {
		t.Fatalf("data = %+v", ce.Data)
	}
	if p := ce.Data["payload"].(map[string]any); p["prompt_id"] != "p1" {
		t.Fatalf("payload = %+v", p)
	}
}

func TestToolsDisabledWithoutManager(t *testing.T) {
	reg := registry.New(zerolog.Nop())
	if err := agent.Register(context.Background(), nil, reg, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Call(context.Background(), "agent.session.list", nil); err == nil {
		t.Fatal("expected disabled error")
	}
}

func TestToolsAndBus(t *testing.T) {
	db, err := store.OpenMemory(zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr, err := manager.New(ctx, "eve", db.DB(), map[string]adapter.Adapter{}, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	bus := supervisor.NewEventBus()
	sub, unsub := bus.SubscribeCloud()
	defer unsub()
	agent.PublishToBus(mgr, bus)

	reg := registry.New(zerolog.Nop())
	if err := agent.Register(ctx, mgr, reg, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Call(ctx, "agent.session.create", map[string]any{"agent": "nope", "dir": "/"}); err == nil {
		t.Fatal("expected unknown agent error")
	}
	if _, err := reg.Call(ctx, "agent.prompt.send", map[string]any{"session": "x"}); err == nil {
		t.Fatal("expected missing text error")
	}
	out, err := reg.Call(ctx, "agent.events.read", map[string]any{"after": 0})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := json.Marshal(out); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sub:
		t.Fatal("unexpected bus event")
	case <-time.After(20 * time.Millisecond):
	}
}
