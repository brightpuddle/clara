// Command clara-tui is the Clara terminal user interface.
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/internal/config"
	"github.com/brightpuddle/clara/tui"
	tuigrpc "github.com/brightpuddle/clara/tui/grpc"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).
		Level(zerolog.WarnLevel).
		With().Timestamp().Logger()

	client, err := tuigrpc.New(cfg.AgentSocketPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect to agent at %s: %v\n", cfg.AgentSocketPath(), err)
		fmt.Fprintln(os.Stderr, "Is clara-agent running? Start it with: make dev-agent")
		os.Exit(1)
	}
	defer client.Close()

	model := tui.New(client, logger)

	p := tea.NewProgram(model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
		os.Exit(1)
	}
}
