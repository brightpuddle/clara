package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/brightpuddle/clara/internal/interpreter"
	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/store"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run <task-file>",
	Short: "Execute an intent file directly (one-off run)",
	Long: `Execute an intent file in the foreground without requiring the background agent.

The file must be a JSON intent document. The agent exits once the intent
reaches a terminal state.

Example:
  clara run ~/.local/share/clara/tasks/my-task.json`,
	Args:         cobra.ExactArgs(1),
	RunE:         runOneOff,
	SilenceUsage: true,
}

func runOneOff(cmd *cobra.Command, args []string) error {
	taskFile := args[0]

	data, err := os.ReadFile(taskFile)
	if err != nil {
		return errors.Wrapf(err, "read task file %q", taskFile)
	}

	// Determine format from extension.
	ext := filepath.Ext(taskFile)
	if ext == ".md" || ext == ".markdown" {
		return fmt.Errorf(
			"markdown task files require the background agent; use 'clara serve' or convert the file to JSON first",
		)
	}

	intent, err := orchestrator.ParseIntent(data)
	if err != nil {
		return errors.Wrapf(err, "parse intent from %q", taskFile)
	}

	logger := buildLogger()

	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return errors.Wrapf(err, "create data dir %q", cfg.DataDir)
	}

	db, err := store.Open(cfg.DBPath(), logger)
	if err != nil {
		return errors.Wrap(err, "open database")
	}
	defer db.Close()

	reg := registry.New(logger)

	for _, srv := range cfg.MCPServers {
		mcpSrv := registry.NewMCPServer(
			srv.Name, srv.Command, srv.Args, srv.ResolvedEnv(), logger,
		)
		reg.AddServer(mcpSrv)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Start MCP servers.
	startCtx, startCancel := context.WithCancel(ctx)
	defer startCancel()
	regErrCh := make(chan error, 1)
	go func() {
		if err := reg.Start(startCtx); err != nil && !errors.Is(err, context.Canceled) {
			regErrCh <- err
		}
		close(regErrCh)
	}()

	it := interpreter.New(reg, logger)
	if err := it.Execute(ctx, intent, intent.InitialState, interpreter.RunOptions{
		RunID: intent.ID + "-oneoff",
	}); err != nil {
		return errors.Wrap(err, "execute intent")
	}

	startCancel() // shut down MCP servers
	<-regErrCh
	return nil
}
