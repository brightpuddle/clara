package tmux

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
)

const Description = "Built-in tmux MCP server for managing terminal sessions."

type Service struct {
	tmuxPath        string
	availabilityErr error
	log             zerolog.Logger
}

func New(log zerolog.Logger) *Service {
	path, err := exec.LookPath("tmux")
	if err != nil {
		err = errors.Wrap(err, "tmux binary not found on PATH")
	}
	return &Service{
		tmuxPath:        path,
		availabilityErr: err,
		log:             log.With().Str("component", "mcp_tmux").Logger(),
	}
}

func (s *Service) NewServer() *server.MCPServer {
	mcpServer := server.NewMCPServer(
		"clara-tmux",
		"0.1.0",
		server.WithToolCapabilities(true),
		server.WithInstructions(Description),
	)

	mcpServer.AddTool(mcp.NewTool(
		"list_sessions",
		mcp.WithDescription("List existing tmux sessions."),
	), s.handleListSessions)

	mcpServer.AddTool(mcp.NewTool(
		"create_session",
		mcp.WithDescription("Create a new detached tmux session running a command."),
		mcp.WithString("name", mcp.Required(), mcp.Description("The name of the tmux session.")),
		mcp.WithString("command", mcp.Required(), mcp.Description("The command to run in the session.")),
	), s.handleCreateSession)

	mcpServer.AddTool(mcp.NewTool(
		"capture_pane",
		mcp.WithDescription("Capture the output of a tmux session's first pane."),
		mcp.WithString("name", mcp.Required(), mcp.Description("The name of the tmux session.")),
		mcp.WithNumber("limit", mcp.Description("Optional: only return the last n lines of output.")),
	), s.handleCapturePane)

	mcpServer.AddTool(mcp.NewTool(
		"kill_session",
		mcp.WithDescription("Kill a tmux session by name."),
		mcp.WithString("name", mcp.Required(), mcp.Description("The name of the tmux session.")),
	), s.handleKillSession)

	return mcpServer
}

func (s *Service) handleListSessions(
	ctx context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	if result := s.ensureAvailable(); result != nil {
		return result, nil
	}

	// Use a custom format for easier parsing
	format := "#{session_name}|#{session_windows}|#{session_created}|#{session_width}|#{session_height}|#{?session_attached,attached,detached}"
	output, err := s.runTmux(ctx, "list-sessions", "-F", format)
	if err != nil {
		// If tmux ls fails with "no server running", it's not really an error for our purposes
		if strings.Contains(err.Error(), "no server running") || strings.Contains(err.Error(), "error connecting to /tmp/tmux") {
			return mcp.NewToolResultStructuredOnly([]any{}), nil
		}
		return toolErrorResult("list_sessions", err), nil
	}

	raw := strings.TrimSpace(string(output))
	if raw == "" {
		return mcp.NewToolResultStructuredOnly([]any{}), nil
	}

	lines := strings.Split(raw, "\n")
	sessions := make([]map[string]any, 0, len(lines))

	for _, line := range lines {
		parts := strings.Split(line, "|")
		if len(parts) != 6 {
			continue
		}
		
		windows, _ := strconv.Atoi(parts[1])
		created, _ := strconv.ParseInt(parts[2], 10, 64)
		width, _ := strconv.Atoi(parts[3])
		height, _ := strconv.Atoi(parts[4])

		sessions = append(sessions, map[string]any{
			"name":       parts[0],
			"windows":    windows,
			"created_at": created,
			"width":      width,
			"height":     height,
			"attached":   parts[5] == "attached",
		})
	}

	return mcp.NewToolResultStructuredOnly(sessions), nil
}

func (s *Service) handleCreateSession(
	ctx context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	if result := s.ensureAvailable(); result != nil {
		return result, nil
	}

	name, _ := req.GetArguments()["name"].(string)
	command, _ := req.GetArguments()["command"].(string)

	if name == "" || command == "" {
		return mcp.NewToolResultError("name and command are required"), nil
	}

	fullCommand := fmt.Sprintf("zsh -c %q", command)
	output, err := s.runTmux(ctx, "new", "-d", "-s", name, fullCommand)
	if err != nil {
		return toolErrorResult("create_session", err), nil
	}

	return mcp.NewToolResultStructuredOnly(map[string]any{
		"name":   name,
		"status": "created",
		"output": strings.TrimSpace(string(output)),
	}), nil
}

func (s *Service) handleCapturePane(
	ctx context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	if result := s.ensureAvailable(); result != nil {
		return result, nil
	}

	name, _ := req.GetArguments()["name"].(string)
	if name == "" {
		return mcp.NewToolResultError("name is required"), nil
	}

	// tmux capture-pane -p -t "{name}:0"
	target := fmt.Sprintf("%s:0", name)
	output, err := s.runTmux(ctx, "capture-pane", "-p", "-t", target)
	if err != nil {
		return toolErrorResult("capture_pane", err), nil
	}

	content := string(output)
	if limitVal, ok := req.GetArguments()["limit"].(float64); ok && limitVal > 0 {
		lines := strings.Split(content, "\n")
		limit := int(limitVal)
		if len(lines) > limit {
			content = strings.Join(lines[len(lines)-limit:], "\n")
		}
	}

	return mcp.NewToolResultStructuredOnly(map[string]any{
		"name":    name,
		"content": content,
	}), nil
}

func (s *Service) handleKillSession(
	ctx context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	if result := s.ensureAvailable(); result != nil {
		return result, nil
	}

	name, _ := req.GetArguments()["name"].(string)
	if name == "" {
		return mcp.NewToolResultError("name is required"), nil
	}

	output, err := s.runTmux(ctx, "kill-session", "-t", name)
	if err != nil {
		return toolErrorResult("kill_session", err), nil
	}

	return mcp.NewToolResultStructuredOnly(map[string]any{
		"name":   name,
		"status": "killed",
		"output": strings.TrimSpace(string(output)),
	}), nil
}

func (s *Service) ensureAvailable() *mcp.CallToolResult {
	if s.availabilityErr == nil {
		return nil
	}
	return mcp.NewToolResultError(s.availabilityErr.Error())
}

func (s *Service) runTmux(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, s.tmuxPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, errors.Wrapf(
			err,
			"run tmux command %q: %s",
			strings.Join(args, " "),
			strings.TrimSpace(string(output)),
		)
	}
	return output, nil
}

func toolErrorResult(tool string, err error) *mcp.CallToolResult {
	return mcp.NewToolResultError(fmt.Sprintf("%s: %v", tool, err))
}
