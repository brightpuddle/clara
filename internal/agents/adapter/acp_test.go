package adapter_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/brightpuddle/clara/internal/agents"
	"github.com/brightpuddle/clara/internal/agents/adapter"
)

func TestPromptStreamsAndEndsTurn(t *testing.T) {
	s, p := openSimple(t, adapter.Quirks{})
	if s.NativeID() != "s1" {
		t.Fatalf("native id %q", s.NativeID())
	}

	if err := s.Prompt(context.Background(), adapter.Prompt{ID: "p1", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	req := p.expect("session/prompt")
	if got := params(req)["sessionId"]; got != "s1" {
		t.Fatalf("prompt session %v", got)
	}

	text := func(s string) msg { return msg{"type": "text", "text": s} }
	p.update("s1", msg{"sessionUpdate": "agent_message_chunk", "content": text("Hel")})
	p.update("s1", msg{"sessionUpdate": "agent_message_chunk", "content": text("lo")})
	p.update("s1", msg{
		"sessionUpdate": "tool_call", "toolCallId": "c1", "name": "shell", "kind": "execute",
		"title": "ls", "status": "in_progress", "locations": []any{msg{"path": "/tmp/x"}},
	})
	p.update("s1", msg{
		"sessionUpdate": "tool_call_update", "toolCallId": "c1", "status": "completed",
		"content": []any{msg{"type": "content", "content": text("out")}},
	})
	p.update("s1", msg{"sessionUpdate": "usage_update", "used": 10, "size": 100,
		"cost": msg{"amount": 0.5}})
	p.update("s1", msg{"sessionUpdate": "session_info_update", "title": "Greeting"})
	p.reply(req, msg{"stopReason": "end_turn", "usage": msg{
		"inputTokens": 3, "outputTokens": 4, "totalTokens": 7}})

	evs := eventsUntilTurnEnd(t, s)
	if len(evs) != 8 {
		t.Fatalf("got %d events: %#v", len(evs), evs)
	}
	d1 := evs[0].(adapter.MessageDelta)
	d2 := evs[1].(adapter.MessageDelta)
	if d1.MessageID == "" || d1.MessageID != d2.MessageID || d1.Text+d2.Text != "Hello" {
		t.Fatalf("deltas %#v %#v", d1, d2)
	}
	done := evs[2].(adapter.MessageDone)
	if done.Text != "Hello" || done.MessageID != d1.MessageID {
		t.Fatalf("done %#v", done)
	}
	tc := evs[3].(adapter.ToolCall)
	if tc.CallID != "c1" || tc.Kind != "execute" || tc.Locations[0] != "/tmp/x" {
		t.Fatalf("tool call %#v", tc)
	}
	if tu := evs[4].(adapter.ToolUpdate); tu.Status != "completed" || tu.OutputSummary != "out" {
		t.Fatalf("tool update %#v", tu)
	}
	if uu := evs[5].(adapter.UsageUpdate); uu.Used != 10 || *uu.Cost != 0.5 {
		t.Fatalf("usage %#v", uu)
	}
	if evs[6].(adapter.TitleUpdate).Title != "Greeting" {
		t.Fatalf("title %#v", evs[6])
	}
	end := evs[7].(adapter.TurnEnded)
	if end.PromptID != "p1" || end.StopReason != agents.StopEndTurn || end.Usage.TotalTokens != 7 {
		t.Fatalf("turn end %#v", end)
	}
}

func TestPromptWhileBusyIsRejectedLocally(t *testing.T) {
	s, p := openSimple(t, adapter.Quirks{})
	ctx := context.Background()
	if err := s.Prompt(ctx, adapter.Prompt{ID: "p1", Text: "a"}); err != nil {
		t.Fatal(err)
	}
	req := p.expect("session/prompt")
	if err := s.Prompt(ctx, adapter.Prompt{ID: "p2", Text: "b"}); !errors.Is(err, adapter.ErrBusy) {
		t.Fatalf("want ErrBusy, got %v", err)
	}
	p.reply(req, msg{"stopReason": "end_turn"})
	eventsUntilTurnEnd(t, s)

	if err := s.Prompt(ctx, adapter.Prompt{ID: "p2", Text: "b"}); err != nil {
		t.Fatalf("idle prompt: %v", err)
	}
	p.expect("session/prompt")
}

func TestPromptErrorEndsTurnWithError(t *testing.T) {
	s, p := openSimple(t, adapter.Quirks{})
	if err := s.Prompt(context.Background(), adapter.Prompt{ID: "p1", Text: "a"}); err != nil {
		t.Fatal(err)
	}
	req := p.expect("session/prompt")
	p.send(msg{"id": req["id"], "error": msg{"code": -32603, "message": "boom"}})

	end := nextEvent(t, s).(adapter.TurnEnded)
	if end.StopReason != agents.StopError || end.Err == "" {
		t.Fatalf("turn end %#v", end)
	}
	if err := s.Prompt(context.Background(), adapter.Prompt{ID: "p2"}); err != nil {
		t.Fatalf("session should accept a new prompt: %v", err)
	}
}

func TestCancelSendsNotificationOnlyWhenBusy(t *testing.T) {
	s, p := openSimple(t, adapter.Quirks{})
	ctx := context.Background()
	if err := s.Cancel(ctx); err != nil { // idle: nothing to cancel
		t.Fatal(err)
	}
	if err := s.Prompt(ctx, adapter.Prompt{ID: "p1", Text: "a"}); err != nil {
		t.Fatal(err)
	}
	req := p.expect("session/prompt") // the idle Cancel sent nothing before this
	if err := s.Cancel(ctx); err != nil {
		t.Fatal(err)
	}
	cancel := p.expect("session/cancel")
	if _, hasID := cancel["id"]; hasID || params(cancel)["sessionId"] != "s1" {
		t.Fatalf("cancel must be a notification for s1: %v", cancel)
	}
	p.reply(req, msg{"stopReason": "cancelled"})
	if end := nextEvent(t, s).(adapter.TurnEnded); end.StopReason != agents.StopCancelled {
		t.Fatalf("turn end %#v", end)
	}
}

func TestLoadDropsReplayedHistory(t *testing.T) {
	dial, p := newPeer(t)
	s, err := open(t, newACP(dial, adapter.Quirks{}),
		adapter.OpenSpec{Dir: "/tmp", ResumeID: "old"}, func() {
			p.reply(p.expect("initialize"), msg{})
			req := p.expect("session/load")
			if params(req)["sessionId"] != "old" {
				t.Errorf("load params %v", params(req))
			}
			text := msg{"type": "text", "text": "history"}
			p.update("old", msg{"sessionUpdate": "user_message_chunk", "content": text})
			p.update("old", msg{"sessionUpdate": "agent_message_chunk", "content": text})
			p.update("old", msg{"sessionUpdate": "tool_call", "toolCallId": "h1"})
			p.reply(req, msg{})
		})
	if err != nil {
		t.Fatal(err)
	}
	if s.NativeID() != "old" {
		t.Fatalf("native id %q", s.NativeID())
	}

	p.update("old", msg{"sessionUpdate": "session_info_update", "title": "fresh"})
	if ev := nextEvent(t, s); ev != (adapter.TitleUpdate{Title: "fresh"}) {
		t.Fatalf("first event after load should be live, got %#v", ev)
	}
}

func TestUpdatesForOtherSessionsAreIgnored(t *testing.T) {
	s, p := openSimple(t, adapter.Quirks{})
	p.update("child", msg{"sessionUpdate": "session_info_update", "title": "child"})
	p.update("s1", msg{"sessionUpdate": "session_info_update", "title": "mine"})
	if ev := nextEvent(t, s); ev != (adapter.TitleUpdate{Title: "mine"}) {
		t.Fatalf("got %#v", ev)
	}
}

func TestAgentRequestsAreAnsweredOrRejected(t *testing.T) {
	_, p := openSimple(t, adapter.Quirks{})

	p.send(msg{"id": 100, "method": "session/request_permission", "params": msg{
		"sessionId": "s1",
		"options": []any{
			msg{"optionId": "no", "kind": "reject_once"},
			msg{"optionId": "yes", "kind": "allow_once"},
		},
	}})
	r := p.recv()
	outcome := r["result"].(msg)["outcome"].(msg)
	if r["id"] != float64(100) || outcome["outcome"] != "selected" || outcome["optionId"] != "yes" {
		t.Fatalf("permission reply %v", r)
	}

	p.send(msg{"id": 101, "method": "fs/read_text_file", "params": msg{}})
	r = p.recv()
	e, _ := r["error"].(msg)
	if r["id"] != float64(101) || e["code"] != float64(-32601) {
		t.Fatalf("unknown method reply %v", r)
	}
}

func TestExitEmitsExitedAndClosesEvents(t *testing.T) {
	s, p := openSimple(t, adapter.Quirks{})
	if err := s.Prompt(context.Background(), adapter.Prompt{ID: "p1"}); err != nil {
		t.Fatal(err)
	}
	p.expect("session/prompt")
	p.update("s1", msg{"sessionUpdate": "agent_message_chunk",
		"content": msg{"type": "text", "text": "partial"}})
	_ = p.out.(interface{ Close() error }).Close() // agent dies mid-turn

	if d := nextEvent(t, s).(adapter.MessageDelta); d.Text != "partial" {
		t.Fatalf("delta %#v", d)
	}
	if d := nextEvent(t, s).(adapter.MessageDone); d.Text != "partial" {
		t.Fatalf("partial text must be flushed, got %#v", d)
	}
	ex := nextEvent(t, s).(adapter.Exited)
	if ex.Err == nil {
		t.Fatal("unrequested exit must carry an error")
	}
	select {
	case ev, ok := <-s.Events():
		if ok {
			t.Fatalf("unexpected event after Exited: %#v", ev)
		}
	case <-time.After(timeout):
		t.Fatal("events not closed")
	}
	if err := s.Prompt(context.Background(), adapter.Prompt{ID: "p2"}); err == nil {
		t.Fatal("prompt on a dead session should fail")
	}
}

func TestCloseExitsCleanly(t *testing.T) {
	s, _ := openSimple(t, adapter.Quirks{})
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	ex := nextEvent(t, s).(adapter.Exited)
	if ex.Err != nil {
		t.Fatalf("requested close should not carry an error: %v", ex.Err)
	}
}

func TestContextCancelClosesSession(t *testing.T) {
	dial, p := newPeer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := make(chan adapter.AgentSession, 1)
	go func() {
		s, _ := newACP(dial, adapter.Quirks{}).Open(ctx, adapter.OpenSpec{Dir: "/tmp"})
		ch <- s
	}()
	p.handshake()
	s := <-ch
	cancel()
	if ex := nextEvent(t, s).(adapter.Exited); ex.Err != nil {
		t.Fatalf("err %v", ex.Err)
	}
}

func TestOpenFailureReleasesProcess(t *testing.T) {
	dial, p := newPeer(t)
	_, err := open(t, newACP(dial, adapter.Quirks{}), adapter.OpenSpec{Dir: "/tmp"}, func() {
		p.reply(p.expect("initialize"), msg{})
		req := p.expect("session/new")
		p.send(msg{"id": req["id"], "error": msg{"code": -32000, "message": "auth required"}})
	})
	if err == nil {
		t.Fatal("want error")
	}
}

func TestOpenRequiresDir(t *testing.T) {
	dial, _ := newPeer(t)
	ad := newACP(dial, adapter.Quirks{})
	if _, err := ad.Open(context.Background(), adapter.OpenSpec{}); err == nil {
		t.Fatal("want error")
	}
}

func TestSetModelUsesQuirk(t *testing.T) {
	cases := map[string]struct {
		q      adapter.Quirks
		method string
		key    string
	}{
		"agy":      {adapter.AgyQuirks, "session/set_model", "modelId"},
		"opencode": {adapter.OpencodeQuirks, "session/set_config_option", "value"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dial, p := newPeer(t)
			spec := adapter.OpenSpec{Dir: "/tmp", Model: "m1"}
			_, err := open(t, newACP(dial, c.q), spec, func() {
				p.handshake()
				req := p.expect(c.method)
				if params(req)["sessionId"] != "s1" || params(req)[c.key] != "m1" {
					t.Errorf("params %v", params(req))
				}
				p.reply(req, msg{})
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSetModelUnsupported(t *testing.T) {
	dial, p := newPeer(t)
	_, err := open(t, newACP(dial, adapter.Quirks{}), adapter.OpenSpec{Dir: "/tmp", Model: "m1"},
		p.handshake)
	if err == nil {
		t.Fatal("want error when the agent has no model selection")
	}
}

func TestQuotaMessageEmitsBlocked(t *testing.T) {
	s, p := openSimple(t, adapter.AgyQuirks)
	if err := s.Prompt(context.Background(), adapter.Prompt{ID: "p1"}); err != nil {
		t.Fatal(err)
	}
	req := p.expect("session/prompt")
	p.update("s1", msg{"sessionUpdate": "agent_message_chunk", "content": msg{
		"type": "text", "text": "Individual quota reached. Resets in 22h38m."}})
	p.reply(req, msg{"stopReason": "end_turn"})

	evs := eventsUntilTurnEnd(t, s)
	var blocked *adapter.Blocked
	for i, ev := range evs {
		if b, ok := ev.(adapter.Blocked); ok {
			blocked = &b
			if _, ok := evs[len(evs)-1].(adapter.TurnEnded); !ok || i > len(evs)-2 {
				t.Fatal("Blocked must precede TurnEnded")
			}
		}
	}
	if blocked == nil || blocked.Kind != "quota" || blocked.ResetsAt == nil {
		t.Fatalf("blocked %#v in %#v", blocked, evs)
	}
	if d := time.Until(*blocked.ResetsAt); d < 22*time.Hour || d > 23*time.Hour {
		t.Fatalf("resets in %v", d)
	}
}

func TestDetectQuota(t *testing.T) {
	if b := adapter.AgyQuirks.DetectBlocked("All good, here is your answer."); b != nil {
		t.Fatalf("false positive %#v", b)
	}
	if b := adapter.AgyQuirks.DetectBlocked("Individual quota reached"); b == nil ||
		b.ResetsAt != nil {
		t.Fatalf("quota without reset time: %#v", b)
	}
}

func TestList(t *testing.T) {
	dial, p := newPeer(t)
	ad := newACP(dial, adapter.Quirks{})
	type res struct {
		l   []adapter.NativeSession
		err error
	}
	ch := make(chan res, 1)
	go func() {
		l, err := ad.List(context.Background())
		ch <- res{l, err}
	}()
	p.reply(p.expect("initialize"), msg{})
	req := p.expect("session/list")
	p.reply(req, msg{
		"sessions": []any{msg{"sessionId": "a", "title": "A", "cwd": "/x",
			"updatedAt": "2026-11-04T18:44:51.880Z"}},
		"nextCursor": "c2",
	})
	req = p.expect("session/list")
	if params(req)["cursor"] != "c2" {
		t.Fatalf("cursor %v", params(req))
	}
	p.reply(req, msg{"sessions": []any{msg{"sessionId": "b", "updatedAt": "garbage"}}})

	r := <-ch
	if r.err != nil {
		t.Fatal(r.err)
	}
	if len(r.l) != 2 || r.l[0].ID != "a" || r.l[0].Dir != "/x" || r.l[0].UpdatedAt.IsZero() ||
		r.l[1].ID != "b" || !r.l[1].UpdatedAt.IsZero() {
		t.Fatalf("sessions %#v", r.l)
	}
}

// Fixture-driven tests replay recordings made against the real agents.

func fixture(name string) string { return filepath.Join("..", "testdata", "acp", name) }

func TestFixtureToolCallsAgy(t *testing.T) {
	dial, p := newPeer(t)
	replay(t, p, fixture("agy_acp.jsonl"))
	s, err := open(t, newACP(dial, adapter.AgyQuirks),
		adapter.OpenSpec{Dir: "/tmp", Model: "gemini-3.8-flash-low"}, func() {})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Prompt(context.Background(), adapter.Prompt{ID: "p1", Text: "go"}); err != nil {
		t.Fatal(err)
	}
	evs := eventsUntilTurnEnd(t, s)

	var calls []adapter.ToolCall
	var updates []adapter.ToolUpdate
	var msgs []adapter.MessageDone
	var title string
	for _, ev := range evs {
		switch e := ev.(type) {
		case adapter.ToolCall:
			calls = append(calls, e)
		case adapter.ToolUpdate:
			updates = append(updates, e)
		case adapter.MessageDone:
			msgs = append(msgs, e)
		case adapter.TitleUpdate:
			title = e.Title
		}
	}
	if len(calls) == 0 || calls[0].Name != "write_file" || calls[0].Kind != "edit" ||
		len(calls[0].Locations) != 1 {
		t.Fatalf("tool calls %#v", calls)
	}
	if len(updates) == 0 || updates[0].Status != "completed" {
		t.Fatalf("tool updates %#v", updates)
	}
	if len(msgs) == 0 || title != "Create and Read Hello File" {
		t.Fatalf("msgs %#v title %q", msgs, title)
	}
	end := evs[len(evs)-1].(adapter.TurnEnded)
	if end.StopReason != agents.StopEndTurn || end.Usage == nil || end.Usage.TotalTokens != 24251 {
		t.Fatalf("turn end %#v", end)
	}
	// History flushed before the tool call that follows it, never after TurnEnded.
	if _, ok := evs[len(evs)-2].(adapter.MessageDone); !ok {
		t.Fatalf("final message should be flushed right before TurnEnded: %#v", evs[len(evs)-2])
	}
}

func TestFixtureCancelThenContinue(t *testing.T) {
	cases := map[string]struct {
		file  string
		q     adapter.Quirks
		model string
		skip  []int // set_model is not sent for opencode (config option instead)
	}{
		"agy":      {"acp_cancel_agy.jsonl", adapter.AgyQuirks, "gemini-3.8-flash-low", nil},
		"opencode": {"acp_cancel_opencode.jsonl", adapter.OpencodeQuirks, "", []int{3}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dial, p := newPeer(t)
			replay(t, p, fixture(c.file), c.skip...)
			s, err := open(t, newACP(dial, c.q), adapter.OpenSpec{Dir: "/tmp", Model: c.model},
				func() {})
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if err := s.Prompt(ctx, adapter.Prompt{ID: "p1", Text: "sleep"}); err != nil {
				t.Fatal(err)
			}
			for {
				if _, ok := nextEvent(t, s).(adapter.ToolCall); ok {
					break
				}
			}
			if err := s.Cancel(ctx); err != nil {
				t.Fatal(err)
			}
			evs := eventsUntilTurnEnd(t, s)
			end := evs[len(evs)-1].(adapter.TurnEnded)
			if end.PromptID != "p1" || end.StopReason != agents.StopCancelled {
				t.Fatalf("turn end %#v", end)
			}

			if err := s.Prompt(ctx, adapter.Prompt{ID: "p2", Text: "ok"}); err != nil {
				t.Fatalf("session must stay usable after cancel: %v", err)
			}
			evs = eventsUntilTurnEnd(t, s)
			end = evs[len(evs)-1].(adapter.TurnEnded)
			if end.PromptID != "p2" || end.StopReason != agents.StopEndTurn {
				t.Fatalf("turn end %#v", end)
			}
			var got string
			for _, ev := range evs {
				if m, ok := ev.(adapter.MessageDone); ok {
					got += m.Text
				}
			}
			if got != "ok" {
				t.Fatalf("reply %q", got)
			}
		})
	}
}

// The busy recording has opencode finishing the first turn while a second
// prompt was rejected. The adapter never sends the second prompt (it is
// rejected locally), so replay the recording without it.
func TestFixtureBusyOpencodeFirstTurnUnaffected(t *testing.T) {
	dial, p := newPeer(t)
	replay(t, p, fixture("acp_busy_opencode.jsonl"), 3, 5)
	s, err := open(t, newACP(dial, adapter.OpencodeQuirks), adapter.OpenSpec{Dir: "/tmp"},
		func() {})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Prompt(ctx, adapter.Prompt{ID: "p1", Text: "sleep"}); err != nil {
		t.Fatal(err)
	}
	err = s.Prompt(ctx, adapter.Prompt{ID: "p2", Text: "second"})
	if !errors.Is(err, adapter.ErrBusy) {
		t.Fatalf("want ErrBusy, got %v", err)
	}
	evs := eventsUntilTurnEnd(t, s)
	end := evs[len(evs)-1].(adapter.TurnEnded)
	if end.PromptID != "p1" || end.StopReason != agents.StopEndTurn {
		t.Fatalf("turn end %#v", end)
	}
	var cost *float64
	for _, ev := range evs {
		if u, ok := ev.(adapter.UsageUpdate); ok {
			cost = u.Cost
		}
	}
	if cost == nil {
		t.Fatal("opencode usage_update with cost should be surfaced")
	}
}
