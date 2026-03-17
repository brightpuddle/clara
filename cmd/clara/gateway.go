package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"reflect"
	"syscall"

	"github.com/brightpuddle/clara/internal/registry"
	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

var gatewayCmd = &cobra.Command{
	Use:   "gateway",
	Short: "Start an MCP server that aggregates all configured Clara tools",
	Long: `Start an MCP server on stdio that aggregates all configured Clara tools.

This command starts the configured MCP server subprocesses, discovers their
tools through Clara's registry, and re-exposes the combined toolset as a single
MCP server for external clients such as Claude Code or Aider.`,
	RunE:         runGateway,
	SilenceUsage: true,
}

func init() {
	rootCmd.AddCommand(gatewayCmd)
}

func runGateway(cmd *cobra.Command, args []string) error {
	logger := buildStderrLogger()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	reg := registry.New(logger)
	for _, srv := range cfg.MCPServers {
		mcpSrv := registry.NewMCPServer(
			srv.Name,
			srv.Description,
			srv.Command,
			srv.Args,
			srv.ResolvedEnv(),
			logger,
		)
		if err := reg.AddServer(mcpSrv); err != nil {
			return errors.Wrapf(err, "register MCP server %q", srv.Name)
		}
	}

	if err := reg.StartServers(ctx); err != nil {
		return errors.Wrap(err, "start MCP servers")
	}
	defer reg.StopServers()

	srv := server.NewMCPServer("clara-gateway", "1.0.0", server.WithToolCapabilities(true))
	for _, tool := range reg.Tools() {
		srv.AddTool(tool.Spec, gatewayToolHandler(reg, tool.Name))
	}

	return serveMCP(ctx, srv)
}

func gatewayToolHandler(
	reg *registry.Registry,
	toolName string,
) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := reg.Call(ctx, toolName, request.GetArguments())
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return gatewayToolResult(result)
	}
}

func gatewayToolResult(result any) (*mcp.CallToolResult, error) {
	switch value := result.(type) {
	case nil:
		return mcp.NewToolResultText(""), nil
	case string:
		return mcp.NewToolResultText(value), nil
	case []byte:
		return mcp.NewToolResultText(string(value)), nil
	}

	kind := reflect.TypeOf(result).Kind()
	switch kind {
	case reflect.Map, reflect.Slice, reflect.Array, reflect.Struct:
		return mcp.NewToolResultJSON(result)
	default:
		return mcp.NewToolResultText(fmt.Sprint(result)), nil
	}
}

func buildStderrLogger() zerolog.Logger {
	level, err := zerolog.ParseLevel(cfg.LogLevelNormalized())
	if err != nil {
		level = zerolog.InfoLevel
	}
	if fi, _ := os.Stderr.Stat(); fi != nil && (fi.Mode()&os.ModeCharDevice) != 0 {
		return zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).
			Level(level).
			With().Timestamp().Logger()
	}
	return zerolog.New(os.Stderr).Level(level).With().Timestamp().Logger()
}
