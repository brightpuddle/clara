// Command clara-agent is the Clara background agent daemon.
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"context"
	"path/filepath"

	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/agent"
	"github.com/brightpuddle/clara/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	logger := buildLogger(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-quit
		logger.Info().Msg("received shutdown signal")
		cancel()
	}()

	a := agent.New(cfg, logger)
	if err := a.Run(ctx); err != nil {
		logger.Fatal().Err(err).Msg("agent failed")
	}
}

func buildLogger(cfg *config.Config) zerolog.Logger {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	level, err := zerolog.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}

	if cfg.LogFile != "" {
		logDir := filepath.Dir(cfg.LogFile)
		if err := os.MkdirAll(logDir, 0o755); err == nil {
			if f, err := os.OpenFile(cfg.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
				return zerolog.New(f).Level(level).With().Timestamp().Logger()
			}
		}
	}
	return zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).Level(level).With().Timestamp().Logger()
}
