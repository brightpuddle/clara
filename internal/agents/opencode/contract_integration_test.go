//go:build integration

package opencode

// Contract test against a real opencode service (opt in: -tags integration).
// Run it on every opencode upgrade. It uses one tiny prompt on a cheap model.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/brightpuddle/clara/internal/agents"
)

// pinnedVersion is the opencode version the adapter was validated against.
const pinnedVersion = "2.2.6"

func TestContractAgainstLiveService(t *testing.T) {
	ep, err := Discover(DefaultStateFile())
	if err != nil {
		t.Skipf("no opencode service: %v", err)
	}
	if ep.Version != pinnedVersion {
		t.Errorf("opencode %s is running, adapter validated against %s: re-check the "+
			"findings in the opencode spike doc, then bump pinnedVersion", ep.Version, pinnedVersion)
	}

	dir := t.TempDir()
	c := &client{stateFile: DefaultStateFile(), hc: &http.Client{}}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/session", map[string]any{
		"title":    "clara contract test",
		"location": map[string]string{"directory": dir},
		"model":    map[string]string{"id": "gemini-3.8-flash", "providerID": "github-copilot"},
	}, &created); err != nil {
		t.Fatal(err)
	}
	sid := created.Data.ID
	t.Cleanup(func() {
		_ = c.do(context.Background(), http.MethodDelete, "/api/session/"+sid, nil, nil)
	})

	rec := &fakeRecorder{}
	m := New(Config{
		Host: "contract", Recorder: rec, Backoff: 100 * time.Millisecond,
		Filter: func(si SessionInfo) bool { return si.ID == sid },
	})
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); _ = m.Run(runCtx) }()
	defer func() { stop(); <-done }()
	rec.waitType(t, agents.TypeSessionCreated, 1)

	// Prompt round trip: user, assistant text and turn end are mirrored from messages.
	if err := m.Send(ctx, sid, "Reply with exactly: ok"); err != nil {
		t.Fatal(err)
	}
	waitForLong(t, "turn end", func() bool { return len(rec.of(agents.TypeTurnEnded)) >= 1 })
	if len(rec.of(agents.TypeMessageUser)) != 1 {
		t.Errorf("user messages = %d, want 1 (types: %v)", len(rec.of(agents.TypeMessageUser)), rec.types())
	}
	as := rec.of(agents.TypeMessageAssistant)
	if len(as) == 0 || !strings.Contains(strings.ToLower(as[0].Payload.(agents.MessageAssistant).Text), "ok") {
		t.Errorf("assistant = %+v", as)
	}
	if te := rec.of(agents.TypeTurnEnded)[0].Payload.(agents.TurnEnded); te.StopReason != agents.StopEndTurn {
		t.Errorf("turn.ended = %+v", te)
	}

	// Forms: create without a model, see the attention, reply once, lose the race the second time.
	var form struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
		ID string `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/session/"+sid+"/form", map[string]any{
		"title": "Pick",
		"fields": []map[string]any{{
			"key": "q0", "type": "string", "title": "Colour", "description": "Red or blue?",
			"options": []map[string]string{{"value": "Red", "label": "Red"}, {"value": "Blue", "label": "Blue"}},
			"custom":  true,
		}},
	}, &form); err != nil {
		t.Fatal(err)
	}
	formID := form.Data.ID
	if formID == "" {
		formID = form.ID
	}
	rec.waitType(t, agents.TypeAttentionRequested, 1)
	if err := m.Reply(ctx, formID, "Red", "contract"); err != nil {
		t.Fatalf("first reply: %v", err)
	}
	if err := m.Reply(ctx, formID, "Blue", "contract"); err == nil {
		t.Error("second reply to a resolved form succeeded")
	}
	if err := c.do(ctx, http.MethodPost, "/api/session/"+sid+"/form/"+formID+"/reply",
		map[string]any{"answer": map[string]string{"q0": "Blue"}}, nil); !isStatus(err, 404, 409) {
		t.Errorf("late reply = %v, want 404 or 409 (the adapter maps both to ErrResolved)", err)
	}

	// Canary: the experimental log endpoint must still not replay. If it starts to,
	// the adapter can use it for lossless catch-up; update the spike doc and design.
	events := 0
	logCtx, logCancel := context.WithTimeout(ctx, 3*time.Second)
	defer logCancel()
	_ = c.stream(logCtx, "/api/experimental/session/"+sid+"/log?after=0", func(d []byte) error {
		var e struct{ Type string }
		_ = json.Unmarshal(d, &e)
		if e.Type != "log.synced" {
			events++
		}
		return nil
	})
	if events != 0 {
		t.Errorf("/log now replays %d events: revisit the catch-up design", events)
	}
}

func waitForLong(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(75 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestContractCreateSession(t *testing.T) {
	if _, err := Discover(DefaultStateFile()); err != nil {
		t.Skipf("no opencode service: %v", err)
	}
	rec := &fakeRecorder{}
	m := New(Config{Host: "contract", Recorder: rec})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	id, err := m.CreateSession(ctx, t.TempDir(), "clara contract create",
		"github-copilot/gemini-3.8-flash", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = m.c.do(context.Background(), http.MethodDelete, "/api/session/"+id, nil, nil)
	})
	cs := rec.of(agents.TypeSessionCreated)
	if len(cs) != 1 || cs[0].Session != id {
		t.Fatalf("created = %+v", cs)
	}
	if got := cs[0].Payload.(agents.SessionCreated).Model; got != "github-copilot/gemini-3.8-flash" {
		t.Errorf("model = %q", got)
	}
}
