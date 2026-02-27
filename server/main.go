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
	"github.com/brightpuddle/clara/server/rag"
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
	cfg := configFromEnv()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Database
	database, err := db.New(ctx, cfg.DBDSN)
	if err != nil {
		return fmt.Errorf("db connect: %w", err)
	}
	defer database.Close()

	if err := database.Migrate(ctx); err != nil {
		return fmt.Errorf("db migrate: %w", err)
	}
	slog.Info("database ready")

	// Embedder
	embedder := rag.NewEmbedder(cfg.OllamaURL, "nomic-embed-text")

	// Temporal client
	temporalClient, err := client.Dial(client.Options{HostPort: cfg.TemporalHost})
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

	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("grpc listen: %w", err)
	}
	go func() {
		slog.Info("gRPC server listening", "addr", cfg.GRPCAddr)
		if err := gs.Serve(lis); err != nil {
			slog.Error("gRPC serve error", "err", err)
		}
	}()

	// HTTP/REST server
	handler := api.NewHandler(database)
	httpSrv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      handler.Router(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}
	go func() {
		slog.Info("HTTP server listening", "addr", cfg.HTTPAddr)
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

type config struct {
	DBDSN        string
	OllamaURL    string
	TemporalHost string
	GRPCAddr     string
	HTTPAddr     string
}

func configFromEnv() config {
	return config{
		DBDSN:        envOr("CLARA_DB_DSN", "postgres://clara:clara@localhost:5432/clara?sslmode=disable"),
		OllamaURL:    envOr("CLARA_OLLAMA_URL", "http://localhost:11434"),
		TemporalHost: envOr("CLARA_TEMPORAL_HOST", "localhost:7233"),
		GRPCAddr:     envOr("CLARA_GRPC_ADDR", ":50051"),
		HTTPAddr:     envOr("CLARA_HTTP_ADDR", ":8080"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
