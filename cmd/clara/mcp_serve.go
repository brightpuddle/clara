package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/brightpuddle/clara/internal/config"
	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
)

var (
	mcpServeProfile string
	mcpServeAllow   []string
	mcpServeDeny    []string
)

var mcpServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Expose a whitelisted subset of Clara's tools as a stdio MCP server",
	Long: `Run Clara as a stdio MCP server, so external agent harnesses (e.g. a
coding assistant) can connect to a curated subset of Clara's tools.

The exposed tools are whitelist-only (default-deny): a tool must match
--profile's configured "include" globs, or an --allow glob, to be exposed at
all. This avoids re-exposing tools the harness already has natively
configured (e.g. its own GitHub MCP server).

Examples:
  clara mcp serve --profile claude-code
  clara mcp serve --allow 'mail.*' --allow 'task.*' --deny 'task.delete'`,
	RunE:         runMCPServe,
	SilenceUsage: true,
}

func init() {
	mcpServeCmd.Flags().StringVar(&mcpServeProfile, "profile", "", "Named profile from mcp_expose_profiles in config.yaml")
	mcpServeCmd.Flags().StringArrayVar(&mcpServeAllow, "allow", nil, "Additional tool name glob to expose (repeatable)")
	mcpServeCmd.Flags().StringArrayVar(&mcpServeDeny, "deny", nil, "Additional tool name glob to exclude (repeatable)")
	mcpCmd.AddCommand(mcpServeCmd)
}

// resolveExposeProfile builds the effective expose profile for a `clara mcp
// serve` invocation: the named profile from config (if any), widened by
// --allow and narrowed by --deny. At least one of profileName or allow must
// be non-empty, so the command never silently falls back to exposing
// everything or nothing.
func resolveExposeProfile(cfg *config.Config, profileName string, allow, deny []string) (config.MCPExposeProfile, error) {
	var profile config.MCPExposeProfile
	if profileName != "" {
		p, ok := cfg.MCPExposeProfiles[profileName]
		if !ok {
			return config.MCPExposeProfile{}, fmt.Errorf("no mcp_expose_profiles entry named %q in config", profileName)
		}
		profile = p
	}

	profile.Include = append(append([]string(nil), profile.Include...), allow...)
	profile.Exclude = append(append([]string(nil), profile.Exclude...), deny...)

	if len(profile.Include) == 0 {
		return config.MCPExposeProfile{}, fmt.Errorf("no tools selected: pass --profile <name> and/or --allow <glob>")
	}

	return profile, nil
}

func runMCPServe(cmd *cobra.Command, args []string) error {
	if !isRunning(cfg.ControlSocketPath()) {
		return fmt.Errorf("clara agent is not running; start it with 'clara serve'")
	}

	profile, err := resolveExposeProfile(cfg, mcpServeProfile, mcpServeAllow, mcpServeDeny)
	if err != nil {
		return err
	}

	specs, err := fetchToolSpecs()
	if err != nil {
		return fmt.Errorf("fetch tool specs: %w", err)
	}

	mcpSrv := mcpserver.NewMCPServer("clara", "1.0.0")
	exposed := 0
	for _, t := range specs {
		if !profile.Allows(t.Name) {
			continue
		}
		t := t
		mcpSrv.AddTool(t, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return callToolViaAgent(t.Name, req)
		})
		exposed++
	}
	if exposed == 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning: no tools matched the selected profile; the MCP server will report an empty tool list")
	}

	return mcpserver.ServeStdio(mcpSrv)
}

// fetchToolSpecs retrieves the full, unfiltered set of registered tool specs
// from the running agent, in raw MCP-wire format.
func fetchToolSpecs() ([]mcp.Tool, error) {
	resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
		Method: ipc.MethodToolList,
		Params: map[string]any{"view": "specs"},
	})
	if err != nil {
		return nil, err
	}

	raw, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, err
	}
	var specs []mcp.Tool
	if err := json.Unmarshal(raw, &specs); err != nil {
		return nil, fmt.Errorf("decode tool specs: %w", err)
	}
	return specs, nil
}

// callToolViaAgent proxies an MCP tool call through to the running Clara
// agent over the control socket.
func callToolViaAgent(name string, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, _ := req.Params.Arguments.(map[string]any)
	if args == nil {
		args = map[string]any{}
	}

	resp, err := sendRawRequest(cfg.ControlSocketPath(), ipc.Request{
		Method: ipc.MethodToolCall,
		Params: map[string]any{"name": name},
		Args:   args,
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if resp.Error != "" {
		return mcp.NewToolResultError(resp.Error), nil
	}

	resBytes, err := json.Marshal(resp.Data)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(resBytes)), nil
}
