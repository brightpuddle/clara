package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	fsmcp "github.com/brightpuddle/clara/internal/mcpserver/fs"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start a built-in MCP server",
	Long: `Start a built-in MCP server on stdio.

Clara ships several MCP servers that can be used directly or configured as
external MCP servers in config.yaml. For example:

  mcp_servers:
    - name: fs
      command: clara
      args: [mcp, fs]
      description: "Built-in filesystem server"

Available servers:
  fs    filesystem operations (read, write, list, search, move, delete)`,
}

var mcpFsCmd = &cobra.Command{
	Use:   "fs",
	Short: "Start the built-in filesystem MCP server",
	Long: fmt.Sprintf(
		"Start the Clara built-in filesystem MCP server on stdio.\n\n%s",
		fsmcp.Description,
	),
	RunE:         runMCPFs,
	SilenceUsage: true,
	// MCP servers run on stdio; skip the PersistentPreRunE config loading.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error { return nil },
}

func init() {
	mcpCmd.AddCommand(mcpFsCmd)
}

func runMCPFs(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	s := fsmcp.New()
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ServeStdio(s)
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}
