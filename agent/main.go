package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/brightpuddle/clara/agent/actions"
	"github.com/brightpuddle/clara/agent/ingest"
	"github.com/brightpuddle/clara/agent/watcher"
)

func main() {
	cfg := loadAgentConfig()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// gRPC client to clara-server
	client, err := ingest.NewClient(cfg.Server.Addr)
	if err != nil {
		slog.Error("failed to connect to server", "err", err, "addr", cfg.Server.Addr)
		os.Exit(1)
	}
	defer client.Close()

	slog.Info("connected to server", "addr", cfg.Server.Addr)

	stats := newAgentStats(cfg.Notes.Dir, cfg.Server.Addr)
	go serveSocket(ctx, stats, cancel)

	// Start markdown watcher
	w, err := watcher.New(cfg.Notes.Dir, client)
	if err != nil {
		slog.Error("failed to create watcher", "err", err)
		os.Exit(1)
	}

	go func() {
		if err := w.Run(ctx); err != nil && ctx.Err() == nil {
			slog.Error("watcher error", "err", err)
		}
	}()

	go func() {
		slog.Info("scanning notes directory", "dir", cfg.Notes.Dir)
		if err := w.Scan(ctx); err != nil {
			slog.Warn("initial scan error", "err", err)
		}
	}()

	ticker := time.NewTicker(cfg.parsedPollInterval())
	defer ticker.Stop()

	executor := actions.NewExecutor()

	slog.Info("agent running", "notes_dir", cfg.Notes.Dir, "poll_interval", cfg.PollInterval)

	for {
		select {
		case <-ctx.Done():
			slog.Info("agent shutting down")
			return
		case <-ticker.C:
			if err := pollAndExecute(ctx, client, executor, stats); err != nil {
				slog.Warn("action poll error", "err", err)
			}
		}
	}
}

func pollAndExecute(ctx context.Context, client *ingest.Client, executor *actions.Executor, stats *agentStats) error {
	pendingActions, err := client.GetPendingActions(ctx)
	if err != nil {
		return err
	}
	for _, action := range pendingActions {
		success := true
		errMsg := ""
		if execErr := executor.Execute(ctx, action); execErr != nil {
			slog.Warn("action execution failed", "id", action.ActionId, "err", execErr)
			success = false
			errMsg = execErr.Error()
		} else {
			slog.Info("action applied", "id", action.ActionId, "path", action.DocumentPath)
			if stats != nil {
				stats.actionsApplied.Add(1)
			}
		}
		if ackErr := client.AckAction(ctx, action.ActionId, success, errMsg); ackErr != nil {
			slog.Warn("ack failed", "id", action.ActionId, "err", ackErr)
		}
	}
	return nil
}
