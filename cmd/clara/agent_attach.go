package main

import (
	"context"
	"encoding/json"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"github.com/brightpuddle/clara/internal/cockpit"
)

var agentAttachCmd = &cobra.Command{
	Use:   "attach [session-query]",
	Short: "Attach interactively to an agent session",
	Long: `Attach interactively to a managed agent session.
Query can be a full session ID, prefix, suffix, or words from the session title.
If omitted, attaches to the most recently active open session.

Inside attach:
  - Type prompt and press Enter to send
  - Press Ctrl-G or type /edit to compose in $EDITOR (Neovim) with full completion
  - /model [name] to view or switch model
  - /interrupt or /cancel to halt current turn or queued prompt
  - /detach or Ctrl-C / Esc to safely detach back to your shell`,
	Args:         cobra.MaximumNArgs(1),
	RunE:         runAgentSessionAttach,
	SilenceUsage: true,
}

func init() {
	agentCmd.AddCommand(agentAttachCmd)
}

// daemonSessionClient implements cockpit.SessionClient by calling daemon tools over IPC.
type daemonSessionClient struct{}

func (c *daemonSessionClient) GetSession(_ context.Context, id string) (cockpit.SessionSummary, error) {
	out, err := callAgentTool("agent.session.get", map[string]any{"session": id})
	if err != nil {
		return cockpit.SessionSummary{}, err
	}
	s, _ := out["session"].(map[string]any)
	if s == nil {
		return cockpit.SessionSummary{}, errors.Newf("session %q not found", id)
	}

	queue, _ := out["queue"].([]any)
	summary := cockpit.SessionSummary{
		ID:        fmt.Sprint(s["id"]),
		Host:      fmt.Sprint(s["host"]),
		Agent:     fmt.Sprint(s["agent"]),
		Dir:       fmt.Sprint(s["dir"]),
		Title:     fmt.Sprint(s["title"]),
		State:     fmt.Sprint(s["state"]),
		Model:     fmt.Sprint(s["model"]),
		QueueLen:  len(queue),
		ActiveTurn: fmt.Sprint(s["active_turn"]),
	}
	return summary, nil
}

func (c *daemonSessionClient) ReadEvents(_ context.Context, session string, after int64) ([]cockpit.EventItem, error) {
	args := map[string]any{
		"session": session,
		"limit":   100,
	}
	if after > 0 {
		args["after"] = after
	}
	out, err := callAgentTool("agent.events.read", args)
	if err != nil {
		return nil, err
	}
	rawEvents, _ := out["events"].([]any)
	var items []cockpit.EventItem
	for _, raw := range rawEvents {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		item := cockpit.EventItem{
			Type: fmt.Sprint(m["type"]),
		}
		if s, ok := m["seq"].(float64); ok {
			item.Seq = int64(s)
		}
		if hs, ok := m["host_seq"].(float64); ok {
			item.HostSeq = int64(hs)
		}
		if p, ok := m["payload"].(map[string]any); ok {
			item.Payload = p
		} else if pStr, ok := m["payload"].(string); ok {
			var parsed map[string]any
			if json.Unmarshal([]byte(pStr), &parsed) == nil {
				item.Payload = parsed
			}
		}
		items = append(items, item)
	}
	return items, nil
}

func (c *daemonSessionClient) SendPrompt(_ context.Context, session, prompt string) error {
	_, err := callAgentTool("agent.prompt.send", map[string]any{
		"session": session,
		"text":    prompt,
		"origin":  "cockpit",
	})
	return err
}

func (c *daemonSessionClient) Interrupt(_ context.Context, session string) error {
	_, err := callAgentTool("agent.turn.interrupt", map[string]any{
		"session": session,
	})
	return err
}

func (c *daemonSessionClient) SetModel(_ context.Context, session, model string) error {
	_, err := callAgentTool("agent.session.set_model", map[string]any{
		"session": session,
		"model":   model,
	})
	return err
}

func runAgentSessionAttach(_ *cobra.Command, args []string) error {
	// 1. Fetch available sessions from daemon
	out, err := callAgentTool("agent.session.list", nil)
	if err != nil {
		return err
	}
	rawSessions, _ := out["sessions"].([]any)
	var summaries []cockpit.SessionSummary
	for _, v := range rawSessions {
		s, _ := v.(map[string]any)
		summaries = append(summaries, cockpit.SessionSummary{
			ID:        fmt.Sprint(s["id"]),
			Host:      fmt.Sprint(s["host"]),
			Agent:     fmt.Sprint(s["agent"]),
			Dir:       fmt.Sprint(s["dir"]),
			Title:     fmt.Sprint(s["title"]),
			State:     fmt.Sprint(s["state"]),
			Model:     fmt.Sprint(s["model"]),
			ActiveTurn: fmt.Sprint(s["active_turn"]),
		})
	}

	if len(summaries) == 0 {
		return errors.New("no active agent sessions found")
	}

	var target cockpit.SessionSummary
	if len(args) > 0 {
		target, err = cockpit.ResolveSession(summaries, args[0])
		if err != nil {
			return err
		}
	} else {
		// Default to newest non-closed session
		var newestOpen *cockpit.SessionSummary
		for i := len(summaries) - 1; i >= 0; i-- {
			if summaries[i].State != "closed" {
				newestOpen = &summaries[i]
				break
			}
		}
		if newestOpen == nil {
			target = summaries[len(summaries)-1]
		} else {
			target = *newestOpen
		}
	}

	client := &daemonSessionClient{}
	model := cockpit.NewAttachModel(client, target)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return errors.Wrap(err, "cockpit attach run error")
	}
	return nil
}
