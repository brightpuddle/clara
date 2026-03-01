package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/brightpuddle/clara/pb"
	"github.com/brightpuddle/clara/server/api"
	"github.com/brightpuddle/clara/server/db"
	grpcserver "github.com/brightpuddle/clara/server/grpc"
	"github.com/brightpuddle/clara/server/web"
	"github.com/brightpuddle/clara/server/workers"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"google.golang.org/grpc"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := loadServerConfig()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Database
	database, err := db.New(ctx, cfg.DB.DSN)
	if err != nil {
		return fmt.Errorf("db connect: %w", err)
	}
	defer database.Close()

	if err := database.Migrate(ctx); err != nil {
		return fmt.Errorf("db migrate: %w", err)
	}
	slog.Info("database ready")

	// Embedder (Ollama or cloud provider based on config)
	embedder, err := buildEmbedder(cfg.AI)
	if err != nil {
		return fmt.Errorf("embedder: %w", err)
	}

	// Temporal client
	temporalClient, err := client.Dial(client.Options{HostPort: cfg.Temporal.Host})
	if err != nil {
		slog.Warn("temporal unavailable, running without workflow engine", "err", err)
		temporalClient = nil
	}

	// Temporal worker (registers workflow + activities)
	if temporalClient != nil {
		acts := workers.NewActivities(database)
		w := worker.New(temporalClient, grpcserver.LinkAnalysisQueue, worker.Options{})
		w.RegisterWorkflow(workers.LinkAnalysisWorkflow)
		w.RegisterActivity(acts)
		if err := w.Start(); err != nil {
			return fmt.Errorf("temporal worker start: %w", err)
		}
		defer w.Stop()
		slog.Info("temporal worker started")
	}

	// gRPC server
	grpcSrv := grpcserver.New(database, embedder, temporalClient)
	gs := grpc.NewServer()
	pb.RegisterIngestServiceServer(gs, grpcSrv)

	lis, err := net.Listen("tcp", cfg.GRPC.Addr)
	if err != nil {
		return fmt.Errorf("grpc listen: %w", err)
	}
	go func() {
		slog.Info("gRPC server listening", "addr", cfg.GRPC.Addr)
		if err := gs.Serve(lis); err != nil {
			slog.Error("gRPC serve error", "err", err)
		}
	}()

	// HTTP/REST server
	handler := api.NewHandler(database)
	webHandler := web.NewWebHandler(database)
	handler.SetWebHandler(webHandler.Router())
	httpSrv := &http.Server{
		Addr:         cfg.HTTP.Addr,
		Handler:      handler.Router(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}
	go func() {
		slog.Info("HTTP server listening", "addr", cfg.HTTP.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP serve error", "err", err)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")

	gs.GracefulStop()
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()
	return httpSrv.Shutdown(shutCtx)
}
