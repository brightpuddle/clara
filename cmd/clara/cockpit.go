package main

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"github.com/brightpuddle/clara/internal/cockpit"
)

var cockpitCmd = &cobra.Command{
	Use:     "cockpit",
	Aliases: []string{"top"},
	Short:   "Full-screen terminal dashboard for managed agent sessions",
	Long: `Full-screen lazygit-style terminal cockpit for Clara agent sessions across all hosts.

Features:
  - Sorted with sessions needing your attention first (blocked, failed, needs_you)
  - Navigate with j/k or arrow keys
  - Press Enter to attach directly to the selected session full-screen
  - Press / to fuzzy-filter by title, project dir, or agent
  - Press x to close a session
  - Press q or Ctrl-C to exit`,
	Args:         cobra.NoArgs,
	RunE:         runCockpitDashboard,
	SilenceUsage: true,
}

func init() {
	rootCmd.AddCommand(cockpitCmd)
	agentCmd.AddCommand(cockpitCmd)
}

// fullDashboardClient implements cockpit.DashboardClient using daemon IPC tool calls.
type fullDashboardClient struct {
	daemonSessionClient
}

func (c *fullDashboardClient) ListSessions(_ context.Context) ([]cockpit.SessionSummary, error) {
	out, err := callAgentTool("agent.session.list", nil)
	if err != nil {
		return nil, err
	}
	rawSessions, _ := out["sessions"].([]any)
	var summaries []cockpit.SessionSummary
	for _, v := range rawSessions {
		s, _ := v.(map[string]any)
		summaries = append(summaries, cockpit.SessionSummary{
			ID:         fmt.Sprint(s["id"]),
			Host:       fmt.Sprint(s["host"]),
			Agent:      fmt.Sprint(s["agent"]),
			Dir:        fmt.Sprint(s["dir"]),
			Title:      fmt.Sprint(s["title"]),
			State:      fmt.Sprint(s["state"]),
			Model:      fmt.Sprint(s["model"]),
			ActiveTurn: fmt.Sprint(s["active_turn"]),
		})
	}
	return summaries, nil
}

func (c *fullDashboardClient) CloseSession(_ context.Context, id string) error {
	_, err := callAgentTool("agent.session.close", map[string]any{"session": id})
	return err
}

func (c *fullDashboardClient) CreateSession(_ context.Context, agent, dir, prompt string) (string, error) {
	req := map[string]any{
		"agent": agent,
		"dir":   dir,
	}
	if prompt != "" {
		req["prompt"] = prompt
	}
	out, err := callAgentTool("agent.session.create", req)
	if err != nil {
		return "", err
	}
	id, _ := out["session"].(string)
	return id, nil
}

func runCockpitDashboard(_ *cobra.Command, _ []string) error {
	client := &fullDashboardClient{}
	model := cockpit.NewDashboardModel(client)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return errors.Wrap(err, "cockpit dashboard run error")
	}
	return nil
}
