// Command clara-server is the Clara AI embedding server.
package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
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
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	logger := buildLogger(cfg)
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

func buildLogger(cfg *config.Config) zerolog.Logger {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	level, err := zerolog.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}

	if cfg.LogFile == "" {
		return zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).Level(level).With().Timestamp().Logger()
	}

	if err := os.MkdirAll(cfg.DataDir+"/logs", 0o755); err == nil {
		if f, err := os.OpenFile(cfg.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			return zerolog.New(f).Level(level).With().Timestamp().Logger()
		}
	}
	return zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).Level(level).With().Timestamp().Logger()
}
