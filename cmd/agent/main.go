// Command clara-agent is the Clara background agent daemon.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/agent"
	"github.com/brightpuddle/clara/internal/config"
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
