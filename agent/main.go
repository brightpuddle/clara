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
	cfg := configFromEnv()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// gRPC client to clara-server
	client, err := ingest.NewClient(cfg.ServerAddr)
	if err != nil {
		slog.Error("failed to connect to server", "err", err, "addr", cfg.ServerAddr)
		os.Exit(1)
	}
	defer client.Close()

	slog.Info("connected to server", "addr", cfg.ServerAddr)

	// Start markdown watcher — sends notes to server on create/modify
	w, err := watcher.New(cfg.NotesDir, client)
	if err != nil {
		slog.Error("failed to create watcher", "err", err)
		os.Exit(1)
	}

	go func() {
		if err := w.Run(ctx); err != nil && ctx.Err() == nil {
			slog.Error("watcher error", "err", err)
		}
	}()

	// Initial full scan
	go func() {
		slog.Info("scanning notes directory", "dir", cfg.NotesDir)
		if err := w.Scan(ctx); err != nil {
			slog.Warn("initial scan error", "err", err)
		}
	}()

	// Poll server for approved actions and execute them
	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	executor := actions.NewExecutor()

	slog.Info("agent running", "notes_dir", cfg.NotesDir, "poll_interval", cfg.PollInterval)

	for {
		select {
		case <-ctx.Done():
			slog.Info("agent shutting down")
			return
		case <-ticker.C:
			if err := pollAndExecute(ctx, client, executor); err != nil {
				slog.Warn("action poll error", "err", err)
			}
		}
	}
}

func pollAndExecute(ctx context.Context, client *ingest.Client, executor *actions.Executor) error {
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
		}
		if ackErr := client.AckAction(ctx, action.ActionId, success, errMsg); ackErr != nil {
			slog.Warn("ack failed", "id", action.ActionId, "err", ackErr)
		}
	}
	return nil
}

type config struct {
	ServerAddr   string
	NotesDir     string
	PollInterval time.Duration
	AgentID      string
}

func configFromEnv() config {
	return config{
		ServerAddr:   envOr("CLARA_SERVER_ADDR", "localhost:50051"),
		NotesDir:     envOr("CLARA_NOTES_DIR", os.ExpandEnv("$HOME/notes")),
		PollInterval: 10 * time.Second,
		AgentID:      envOr("CLARA_AGENT_ID", "default"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
