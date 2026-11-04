package agents

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEventRoundTrip(t *testing.T) {
	ev, err := NewEvent("eve", "ses_1", TypeTurnEnded, TurnEnded{
		TurnID:     "t1",
		StopReason: StopEndTurn,
		ToolsSummary: ToolsSummary{
			Count: 2,
			Last:  []string{"read main.go"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ev.Host != "eve" || ev.Session != "ses_1" || ev.Type != TypeTurnEnded {
		t.Fatalf("unexpected envelope: %+v", ev)
	}
	if ev.ID == "" || ev.TS.IsZero() {
		t.Fatalf("id and ts must be set: %+v", ev)
	}

	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var back Event
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	p, err := back.Decode()
	if err != nil {
		t.Fatal(err)
	}
	got, ok := p.(TurnEnded)
	if !ok {
		t.Fatalf("decoded %T, want TurnEnded", p)
	}
	if got.StopReason != StopEndTurn || got.ToolsSummary.Count != 2 {
		t.Fatalf("payload mismatch: %+v", got)
	}
}

func TestDecodeEveryType(t *testing.T) {
	for _, typ := range AllEventTypes() {
		p := newPayload(typ)
		if p == nil {
			t.Fatalf("no payload registered for %q", typ)
		}
		ev, err := NewEvent("eve", "s", typ, p)
		if err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
		if _, err := ev.Decode(); err != nil {
			t.Fatalf("%s: decode: %v", typ, err)
		}
	}
}

func TestDecodeUnknownType(t *testing.T) {
	ev := Event{Type: "bogus", Payload: json.RawMessage(`{}`)}
	if _, err := ev.Decode(); err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestNewEventRejectsMismatchedPayload(t *testing.T) {
	if _, err := NewEvent("eve", "s", TypeTurnStarted, TurnEnded{}); err == nil {
		t.Fatal("expected payload/type mismatch error")
	}
}

func TestDeltaIsNotLoggable(t *testing.T) {
	if TypeMessageDelta.Logged() {
		t.Fatal("message.delta must not be logged")
	}
	if !TypeMessageAssistant.Logged() {
		t.Fatal("message.assistant must be logged")
	}
}

func TestSessionStateValid(t *testing.T) {
	for _, s := range []SessionState{
		StateStarting, StateIdle, StateRunning, StateBlocked, StateFailed, StateClosed,
	} {
		if !s.Valid() {
			t.Fatalf("%q should be valid", s)
		}
	}
	if SessionState("nope").Valid() {
		t.Fatal("unknown state must be invalid")
	}
}

func TestCommandRoundTrip(t *testing.T) {
	raw := []byte(`{"command_id":"c1","device":"d1","name":"prompt.send",` +
		`"args":{"session":"s1","text":"hi"}}`)
	var c Command
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	args, err := c.DecodeArgs()
	if err != nil {
		t.Fatal(err)
	}
	ps, ok := args.(PromptSend)
	if !ok || ps.Session != "s1" || ps.Text != "hi" {
		t.Fatalf("got %#v", args)
	}
}

func TestCommandUnknown(t *testing.T) {
	c := Command{Name: "nope", Args: json.RawMessage(`{}`)}
	if _, err := c.DecodeArgs(); err == nil {
		t.Fatal("expected error")
	}
}

func TestEventTimestampUTC(t *testing.T) {
	ev, err := NewEvent("eve", "", TypeHostStatus, HostStatus{Online: true, LastSeen: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if ev.TS.Location() != time.UTC {
		t.Fatal("ts must be UTC")
	}
}

func TestNewIDSortableAndUnique(t *testing.T) {
	a := NewID()
	time.Sleep(2 * time.Millisecond)
	b := NewID()
	if len(a) != 26 || a >= b {
		t.Fatalf("ids not sortable: %q %q", a, b)
	}
}
