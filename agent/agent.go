// Package agent implements the Clara Agent daemon.
package agent

import (
	"context"
	"os"
	"time"

	"github.com/rs/zerolog"

	agentv1 "github.com/brightpuddle/clara/gen/agent/v1"
	artifactv1 "github.com/brightpuddle/clara/gen/artifact/v1"
	agentgrpc "github.com/brightpuddle/clara/agent/grpc"
	"github.com/brightpuddle/clara/agent/ingestor"
	"github.com/brightpuddle/clara/agent/reminders"
	"github.com/brightpuddle/clara/agent/watcher"
	"github.com/brightpuddle/clara/internal/config"
	"github.com/brightpuddle/clara/internal/db"
	"github.com/brightpuddle/clara/internal/embedding"
)

// Agent orchestrates all background workers and gRPC services.
type Agent struct {
	cfg    *config.Config
	logger zerolog.Logger
}

// New creates a new Agent.
func New(cfg *config.Config, logger zerolog.Logger) *Agent {
	return &Agent{cfg: cfg, logger: logger}
}

// Run starts the agent and blocks until ctx is cancelled.
func (a *Agent) Run(ctx context.Context) error {
	a.logger.Info().Msg("starting clara-agent")

	// Ensure data dir exists.
	if err := os.MkdirAll(a.cfg.DataDir, 0o755); err != nil {
		return err
	}

	// Remove stale socket if present.
	_ = os.Remove(a.cfg.AgentSocketPath())

	// Open local DB.
	database, err := db.Open(a.cfg.DBPath())
	if err != nil {
		return err
	}
	defer database.Close()

	// Connect to server.
	serverClient, serverConn, err := agentgrpc.DialServer(a.cfg.Server.Addr)
	if err != nil {
		a.logger.Warn().Err(err).Str("addr", a.cfg.Server.Addr).Msg("could not connect to server; operating without AI features")
	}
	if serverConn != nil {
		defer serverConn.Close()
	}

	// Create the embedding client.
	embedder := embedding.New(a.cfg.Ollama.URL, a.cfg.Ollama.EmbedModel)

	// Create ingestion pipeline.
	ing := ingestor.New(embedder, serverClient, a.cfg.Agent.IngestConcurrency, a.logger)

	// Set up the gRPC server that serves the TUI.
	agentSrv := agentgrpc.NewAgentServer(database, serverClient, a.logger)

	// Try to connect to native worker (optional).
	if _, err := os.Stat(a.cfg.NativeSocketPath()); err == nil {
		nativeClient, nativeConn, err := agentgrpc.DialNative(a.cfg.NativeSocketPath())
		if err != nil {
			a.logger.Warn().Err(err).Msg("could not connect to native worker")
		} else {
			defer nativeConn.Close()
			agentSrv.SetNativeClient(nativeClient)

			// Start reminders sync worker.
			remindersWorker := reminders.New(nativeClient, database, a.logger)
			go func() {
				remindersWorker.Run(ctx)
			}()
			go a.forwardNotifications(ctx, agentSrv, remindersWorker.Notifications())
		}
	} else {
		a.logger.Info().Msg("native worker socket not found; skipping reminders integration")
	}

	// Start filesystem watcher.
	if len(a.cfg.Agent.WatchDirs) > 0 {
		fsWatcher, err := watcher.New(a.cfg.Agent.WatchDirs, a.logger)
		if err != nil {
			a.logger.Warn().Err(err).Msg("could not create fs watcher")
		} else {
			defer fsWatcher.Close()
			if err := fsWatcher.Start(ctx); err != nil {
				a.logger.Warn().Err(err).Msg("could not start fs watcher")
			} else {
				go ing.Run(ctx, fsWatcher.Events())
				go a.forwardNotifications(ctx, agentSrv, ing.Notifications())
				// Ingest any files that already exist in the watch directories.
				go ing.ScanDirs(ctx, a.cfg.Agent.WatchDirs)
			}
		}
	}

	// Start the gRPC server for the TUI.
	lis, grpcSrv, err := agentgrpc.ListenUnix(a.cfg.AgentSocketPath())
	if err != nil {
		return err
	}
	agentv1.RegisterAgentServiceServer(grpcSrv, agentSrv)

	go func() {
		a.logger.Info().Str("socket", a.cfg.AgentSocketPath()).Msg("agent gRPC server listening")
		if err := grpcSrv.Serve(lis); err != nil {
			a.logger.Error().Err(err).Msg("agent grpc serve error")
		}
	}()

	<-ctx.Done()
	a.logger.Info().Msg("shutting down agent")
	grpcSrv.GracefulStop()
	_ = os.Remove(a.cfg.AgentSocketPath())
	// brief delay for graceful shutdown
	time.Sleep(100 * time.Millisecond)
	return nil
}

// forwardNotifications converts artifact notifications to ArtifactEvents
// and broadcasts them to all TUI subscribers.
func (a *Agent) forwardNotifications(ctx context.Context, srv *agentgrpc.AgentServer, ch <-chan *artifactv1.Artifact) {
	for {
		select {
		case <-ctx.Done():
			return
		case artifact, ok := <-ch:
			if !ok {
				return
			}
			srv.Broadcast(&agentv1.ArtifactEvent{
				Type:     agentv1.EventType_EVENT_TYPE_CREATED,
				Artifact: artifact,
			})
		}
	}
}
