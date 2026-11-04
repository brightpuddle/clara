//go:build integration

package manager_test

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/internal/agents"
	"github.com/brightpuddle/clara/internal/agents/adapter"
	"github.com/brightpuddle/clara/internal/agents/manager"
	"github.com/brightpuddle/clara/internal/store"
)

// Run with: go test -tags integration ./internal/agents/manager -run Real -v
// Each agent needs a cheap model, set via CLARA_TEST_AGY_MODEL and
// CLARA_TEST_OPENCODE_MODEL; an agent without one is skipped.
func TestRealAgentTurn(t *testing.T) {
	cases := []struct {
		name  string
		build func(string) adapter.Adapter
		env   string
	}{
		{"agy", func(b string) adapter.Adapter { return adapter.NewAgy(b, zerolog.Nop()) }, "CLARA_TEST_AGY_MODEL"},
		{"opencode", func(b string) adapter.Adapter { return adapter.NewOpencode(b, zerolog.Nop()) }, "CLARA_TEST_OPENCODE_MODEL"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			model := os.Getenv(c.env)
			if model == "" {
				t.Skipf("%s not set", c.env)
			}
			bin, err := exec.LookPath(c.name)
			if err != nil {
				t.Skipf("%s not installed", c.name)
			}
			db, err := store.OpenMemory(zerolog.Nop())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			m, err := manager.New(ctx, "test", db.DB(),
				map[string]adapter.Adapter{c.name: c.build(bin)}, zerolog.Nop())
			if err != nil {
				t.Fatal(err)
			}
			id, err := m.Create(ctx, agents.SessionCreate{
				Agent: c.name, Dir: t.TempDir(), Model: model, Kind: agents.KindTask,
				Prompt: "Reply with the single word: pong. Do not use any tools.",
			})
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(2 * time.Minute)
			for {
				s, _ := m.Get(id)
				if s.LastStopReason == agents.StopEndTurn && s.State == agents.StateIdle {
					break
				}
				if s.State == agents.StateFailed || s.State == agents.StateBlocked {
					t.Fatalf("session %s: %s", s.State, s.Reason)
				}
				if time.Now().After(deadline) {
					t.Fatalf("turn did not finish; state %s", s.State)
				}
				time.Sleep(200 * time.Millisecond)
			}
			evs, err := m.ReadEvents(ctx, id, 0, 1000)
			if err != nil {
				t.Fatal(err)
			}
			var sawReply bool
			for _, e := range evs {
				if e.Type == agents.TypeMessageAssistant {
					sawReply = true
				}
			}
			if !sawReply {
				t.Fatal("no assistant message logged")
			}
			if err := m.Close(ctx, id); err != nil {
				t.Fatal(err)
			}
		})
	}
}
