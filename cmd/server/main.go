// Command clara-server is the Clara AI embedding server.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"

	"github.com/brightpuddle/clara/internal/config"
	"github.com/brightpuddle/clara/internal/db"
	"github.com/brightpuddle/clara/internal/service"

	serverv1 "github.com/brightpuddle/clara/gen/server/v1"
	claraserver "github.com/brightpuddle/clara/server"
	"github.com/brightpuddle/clara/server/store"
)

type serverRunner struct {
	cfg    *config.Config
	logger zerolog.Logger
}

func (r *serverRunner) Run(ctx context.Context) error {
	if err := os.MkdirAll(r.cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	database, err := db.Open(r.cfg.DBPath())
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	s := store.New(database)
	srv := claraserver.New(s, r.logger)

	grpcServer := grpc.NewServer()
	serverv1.RegisterServerServiceServer(grpcServer, srv)

	lis, err := net.Listen("tcp", r.cfg.Server.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", r.cfg.Server.Addr, err)
	}

	errCh := make(chan error, 1)
	go func() {
		r.logger.Info().Str("addr", r.cfg.Server.Addr).Msg("clara-server listening")
		if err := grpcServer.Serve(lis); err != nil {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		r.logger.Info().Msg("shutting down server")
		grpcServer.GracefulStop()
		return nil
	case err := <-errCh:
		return fmt.Errorf("grpc serve: %w", err)
	}
}

func main() {
	debugFlag := flag.Bool("debug", false, "write logs to both file and stderr (console)")
	flag.Parse()

	if err := config.WriteDefaultConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write default config: %v\n", err)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	logger := buildLogger(cfg, *debugFlag)
	log.Logger = logger

	runner := &serverRunner{cfg: cfg, logger: logger}

	// Handle service commands. If no command is provided, it runs the service.
	cmd := flag.Arg(0)
	handled, err := service.HandleCommand(runner, service.Config{
		Name:        "clara-server",
		DisplayName: "Clara Server",
		Description: "Clara AI embedding server",
		UserName:    "", // Default to current user.
		Arguments:   []string{},
	}, logger, cmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "service command %q failed: %v\n", cmd, err)
		os.Exit(1)
	}
	if !handled {
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		os.Exit(1)
	}
}

func buildLogger(cfg *config.Config, debug bool) zerolog.Logger {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	level, err := zerolog.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}
	if debug {
		level = zerolog.DebugLevel
	}

	console := zerolog.ConsoleWriter{Out: os.Stderr}

	if cfg.LogFile != "" {
		logDir := filepath.Dir(cfg.LogFile)
		if mkErr := os.MkdirAll(logDir, 0o755); mkErr == nil {
			if f, openErr := os.OpenFile(cfg.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); openErr == nil {
				if debug {
					multi := zerolog.MultiLevelWriter(console, f)
					return zerolog.New(multi).Level(level).With().Timestamp().Logger()
				}
				return zerolog.New(f).Level(level).With().Timestamp().Logger()
			}
		}
	}

	return zerolog.New(console).Level(level).With().Timestamp().Logger()
}

var _ io.Writer = os.Stderr
