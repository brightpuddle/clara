// Command clara-agent is the Clara background agent daemon.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/agent"
	"github.com/brightpuddle/clara/internal/config"
	"github.com/brightpuddle/clara/internal/service"
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

	a := agent.New(cfg, logger)

	// Handle service commands. If no command is provided, it runs the service.
	cmd := flag.Arg(0)
	handled, err := service.HandleCommand(a, service.Config{
		Name:        "clara-agent",
		DisplayName: "Clara Agent",
		Description: "Clara background agent daemon",
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
