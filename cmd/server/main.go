// Command clara-server is the Clara AI embedding server.
package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"

	"github.com/brightpuddle/clara/internal/config"
	"github.com/brightpuddle/clara/internal/db"

	serverv1 "github.com/brightpuddle/clara/gen/server/v1"
	claraserver "github.com/brightpuddle/clara/server"
	"github.com/brightpuddle/clara/server/store"
)

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

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		logger.Fatal().Err(err).Msg("create data dir")
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		logger.Fatal().Err(err).Msg("open database")
	}
	defer database.Close()

	s := store.New(database)
	srv := claraserver.New(s, logger)

	grpcServer := grpc.NewServer()
	serverv1.RegisterServerServiceServer(grpcServer, srv)

	lis, err := net.Listen("tcp", cfg.Server.Addr)
	if err != nil {
		logger.Fatal().Err(err).Str("addr", cfg.Server.Addr).Msg("listen")
	}

	go func() {
		logger.Info().Str("addr", cfg.Server.Addr).Msg("clara-server listening")
		if err := grpcServer.Serve(lis); err != nil {
			logger.Error().Err(err).Msg("grpc serve")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info().Msg("shutting down server")
	grpcServer.GracefulStop()
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
					// Write to both file and console when --debug.
					multi := zerolog.MultiLevelWriter(console, f)
					return zerolog.New(multi).Level(level).With().Timestamp().Logger()
				}
				return zerolog.New(f).Level(level).With().Timestamp().Logger()
			}
		}
	}

	return zerolog.New(console).Level(level).With().Timestamp().Logger()
}

// ensure io is used (MultiLevelWriter needs io.Writer).
var _ io.Writer = os.Stderr
