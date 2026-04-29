package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/brightpuddle/clara/pkg/contract"
	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/mcp"
)

const description = "Tmux integration: manage terminal sessions."

// Tmux implements contract.TmuxIntegration.
type Tmux struct {
	tmuxPath string
}

// newTmux returns a Tmux instance, resolving the tmux binary path.
// Returns an error if tmux is not found on PATH.
func newTmux() (*Tmux, error) {
	path, err := exec.LookPath("tmux")
	if err != nil {
		return nil, errors.Wrap(err, "tmux binary not found on PATH")
	}
	return &Tmux{tmuxPath: path}, nil
}

func (t *Tmux) Configure(_ []byte) error { return nil }

func (t *Tmux) Description() (string, error) { return description, nil }

func (t *Tmux) Tools() ([]byte, error) {
	tools := []mcp.Tool{
		mcp.NewTool(
			"session.list",
			mcp.WithDescription("List existing tmux sessions."),
		),
		mcp.NewTool(
			"session.create",
			mcp.WithDescription("Create a new detached tmux session running a command."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Name of the tmux session.")),
			mcp.WithString(
				"command",
				mcp.Required(),
				mcp.Description("Command to run in the session."),
			),
		),
		mcp.NewTool(
			"session.kill",
			mcp.WithDescription("Kill a tmux session by name."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Name of the tmux session.")),
		),
		mcp.NewTool(
			"pane.capture",
			mcp.WithDescription("Capture the output of a tmux session's first pane."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Name of the tmux session.")),
			mcp.WithNumber(
				"limit",
				mcp.Description("Only return the last N lines of output (0 = all)."),
			),
		),
	}
	return json.Marshal(tools)
}

// --- Tool args ---

type sessionCreateArgs struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

type sessionKillArgs struct {
	Name string `json:"name"`
}

type paneCaptureArgs struct {
	Name  string  `json:"name"`
	Limit float64 `json:"limit"`
}

func (t *Tmux) CallTool(name string, args []byte) ([]byte, error) {
	switch name {
	case "session.list":
		sessions, err := t.ListSessions()
		if err != nil {
			return nil, err
		}
		return json.Marshal(sessions)

	case "session.create":
		var a sessionCreateArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, errors.Wrap(err, "unmarshal session.create args")
		}
		if err := t.CreateSession(a.Name, a.Command); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"name": a.Name, "status": "created"})

	case "session.kill":
		var a sessionKillArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, errors.Wrap(err, "unmarshal session.kill args")
		}
		if err := t.KillSession(a.Name); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"name": a.Name, "status": "killed"})

	case "pane.capture":
		var a paneCaptureArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, errors.Wrap(err, "unmarshal pane.capture args")
		}
		content, err := t.CapturePane(a.Name, int(a.Limit))
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"name": a.Name, "content": content})

	default:
		return nil, errors.Newf("unknown tool: %q", name)
	}
}

// --- Typed interface methods ---

func (t *Tmux) ListSessions() ([]contract.TmuxSession, error) {
	format := "#{session_name}|#{session_windows}|#{session_created}" +
		"|#{session_width}|#{session_height}|#{?session_attached,attached,detached}"
	output, err := t.run(context.Background(), "list-sessions", "-F", format)
	if err != nil {
		// No server running means zero sessions, not an error.
		msg := err.Error()
		if strings.Contains(msg, "no server running") ||
			strings.Contains(msg, "error connecting to /tmp/tmux") {
			return []contract.TmuxSession{}, nil
		}
		return nil, err
	}

	raw := strings.TrimSpace(output)
	if raw == "" {
		return []contract.TmuxSession{}, nil
	}

	var sessions []contract.TmuxSession
	for _, line := range strings.Split(raw, "\n") {
		parts := strings.Split(line, "|")
		if len(parts) != 6 {
			continue
		}
		windows, _ := strconv.Atoi(parts[1])
		created, _ := strconv.ParseInt(parts[2], 10, 64)
		width, _ := strconv.Atoi(parts[3])
		height, _ := strconv.Atoi(parts[4])
		sessions = append(sessions, contract.TmuxSession{
			Name:      parts[0],
			Windows:   windows,
			CreatedAt: created,
			Width:     width,
			Height:    height,
			Attached:  parts[5] == "attached",
		})
	}
	return sessions, nil
}

func (t *Tmux) CreateSession(name, command string) error {
	fullCommand := fmt.Sprintf("zsh -c %q", command)
	_, err := t.run(context.Background(), "new", "-d", "-s", name, fullCommand)
	return err
}

func (t *Tmux) CapturePane(name string, limit int) (string, error) {
	target := fmt.Sprintf("%s:0", name)
	output, err := t.run(context.Background(), "capture-pane", "-p", "-t", target)
	if err != nil {
		return "", err
	}
	if limit > 0 {
		lines := strings.Split(output, "\n")
		if len(lines) > limit {
			output = strings.Join(lines[len(lines)-limit:], "\n")
		}
	}
	return output, nil
}

func (t *Tmux) KillSession(name string) error {
	_, err := t.run(context.Background(), "kill-session", "-t", name)
	return err
}

// run executes a tmux subcommand and returns combined stdout+stderr as a string.
func (t *Tmux) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, t.tmuxPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", errors.Wrapf(
			err,
			"tmux %s: %s",
			strings.Join(args, " "),
			strings.TrimSpace(string(out)),
		)
	}
	return string(out), nil
}

// unavailableStub is returned when tmux is not found on PATH. Every method
// returns the original lookup error so callers receive a clear message.
type unavailableStub struct{ err error }

func (s *unavailableStub) Configure(_ []byte) error                      { return s.err }
func (s *unavailableStub) Description() (string, error)                  { return "", s.err }
func (s *unavailableStub) Tools() ([]byte, error)                        { return nil, s.err }
func (s *unavailableStub) CallTool(_ string, _ []byte) ([]byte, error)   { return nil, s.err }
func (s *unavailableStub) ListSessions() ([]contract.TmuxSession, error) { return nil, s.err }
func (s *unavailableStub) CreateSession(_, _ string) error               { return s.err }
func (s *unavailableStub) CapturePane(_ string, _ int) (string, error)   { return "", s.err }
func (s *unavailableStub) KillSession(_ string) error                    { return s.err }
