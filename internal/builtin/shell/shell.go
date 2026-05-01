// Package shell provides the built-in shell integration as an in-process tool.
// It registers a single "shell.run" tool that executes zsh commands.
package shell

import (
	"context"
	"os/exec"

	"github.com/brightpuddle/clara/internal/registry"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"
)

const description = "Built-in shell: run local commands."

// Register adds all shell tools into reg under the "shell" namespace.
// cfg may contain future shell-specific configuration; it is currently unused.
func Register(
	_ context.Context,
	_ map[string]any,
	reg *registry.Registry,
	log zerolog.Logger,
) error {
	log.Debug().Msg("registering shell builtin")

	reg.RegisterNamespaceDescription("shell", description)

	reg.RegisterWithSpec(
		mcp.NewTool(
			"shell.run",
			mcp.WithDescription("Run a shell command"),
			mcp.WithString(
				"command",
				mcp.Required(),
				mcp.Description("The shell command to run"),
			),
		),
		func(ctx context.Context, args map[string]any) (any, error) {
			command, _ := args["command"].(string)

			cmd := exec.CommandContext(ctx, "zsh", "-c", command)
			out, err := cmd.CombinedOutput()

			exitCode := 0
			if err != nil {
				if exitError, ok := err.(*exec.ExitError); ok {
					exitCode = exitError.ExitCode()
					// Non-zero exit is not a Go error; the caller inspects exit_code.
				} else {
					return nil, err
				}
			}

			return map[string]any{
				"output":    string(out),
				"exit_code": exitCode,
			}, nil
		},
	)

	return nil
}
