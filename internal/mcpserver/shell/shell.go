package shell

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
)

// Description is the human-readable summary shown in clara tool list.
const Description = "Built-in shell server: run shell commands."

// New creates a configured MCP server with all shell tools registered.
func New(log zerolog.Logger) *server.MCPServer {
	s := server.NewMCPServer(
		"clara-shell",
		"0.1.0",
		server.WithToolCapabilities(true),
		server.WithInstructions(Description),
	)

	s.AddTool(mcp.NewTool("run",
		mcp.WithDescription("Run a shell command and return its output."),
		mcp.WithString("command",
			mcp.Required(),
			mcp.Description("The shell command to run."),
		),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleRun(ctx, request, log)
	})

	return s
}

func handleRun(ctx context.Context, request mcp.CallToolRequest, log zerolog.Logger) (*mcp.CallToolResult, error) {
	command, err := stringArg(request, "command")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Even if there's an error, the output might contain useful stderr
		return mcp.NewToolResultError(fmt.Sprintf("%s\n%v", string(output), err)), nil
	}

	return mcp.NewToolResultText(strings.TrimSpace(string(output))), nil
}

// stringArg extracts a required string argument from a tool call request.
func stringArg(req mcp.CallToolRequest, name string) (string, error) {
	val, ok := req.GetArguments()[name]
	if !ok {
		return "", fmt.Errorf("missing required argument: %q", name)
	}
	s, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string", name)
	}
	return s, nil
}
