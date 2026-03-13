package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	dbmcp "github.com/brightpuddle/clara/internal/mcpserver/db"
	fsmcp "github.com/brightpuddle/clara/internal/mcpserver/fs"
	taskwmcp "github.com/brightpuddle/clara/internal/mcpserver/taskwarrior"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
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

    - name: db
      command: clara
      args: [mcp, db, ~/.local/share/clara/data.db]
      description: "Built-in SQLite tool server"

Available servers:
  fs    filesystem operations (read, write, list, search, move, delete)
  db    SQLite query, exec, and vector-search tools
  taskwarrior    Taskwarrior CRUD, filtering, and due-task helpers`,
}

var mcpFsCmd = &cobra.Command{
	Use:   "fs",
	Short: "Start the built-in filesystem MCP server",
	Long: fmt.Sprintf(
		"Start the Clara built-in filesystem MCP server on stdio.\n\n%s",
		fsmcp.Description,
	),
	RunE:              runMCPFs,
	SilenceUsage:      true,
	PersistentPreRunE: skipConfigLoad,
}

var mcpDBCmd = &cobra.Command{
	Use:   "db [path]",
	Short: "Start the built-in SQLite MCP server",
	Long: fmt.Sprintf(
		"Start the Clara built-in SQLite MCP server on stdio.\n\n%s\n\nIf no path is provided, the server uses an in-memory database.",
		dbmcp.Description,
	),
	Args:              cobra.MaximumNArgs(1),
	RunE:              runMCPDB,
	SilenceUsage:      true,
	PersistentPreRunE: skipConfigLoad,
}

var mcpTaskwarriorCmd = &cobra.Command{
	Use:   "taskwarrior",
	Short: "Start the built-in Taskwarrior MCP server",
	Long: fmt.Sprintf(
		"Start the Clara built-in Taskwarrior MCP server on stdio.\n\n%s",
		taskwmcp.Description,
	),
	RunE:              runMCPTaskwarrior,
	SilenceUsage:      true,
	PersistentPreRunE: skipConfigLoad,
}

func init() {
	mcpCmd.AddCommand(mcpFsCmd, mcpDBCmd, mcpTaskwarriorCmd)
}

func runMCPFs(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return serveMCP(ctx, fsmcp.New())
}

func runMCPDB(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	path := ""
	if len(args) == 1 {
		path = args[0]
	}

	svc, err := dbmcp.Open(path, zerolog.Nop())
	if err != nil {
		return err
	}
	defer svc.Close()

	return serveMCP(ctx, svc.NewServer())
}

func runMCPTaskwarrior(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	return serveMCP(ctx, taskwmcp.New(zerolog.Nop()).NewServer())
}

func serveMCP(ctx context.Context, srv *server.MCPServer) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ServeStdio(srv)
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}

func skipConfigLoad(cmd *cobra.Command, args []string) error {
	return nil
}
