package tui

import (
	"context"
	"fmt"

	"github.com/brightpuddle/clara/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

func Run(cfg *config.Config) error {
	client := NewIPCClient(cfg)
	service := NewNotificationService(client)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer service.Close()

	startLines := []string{}
	if client.IsRunning() {
		if err := service.Start(ctx); err != nil {
			startLines = append(
				startLines,
				fmt.Sprintf("Failed to register tui MCP service: %v", err),
			)
		}
	}

	model := NewModel(cfg, client, service, startLines)
	program := tea.NewProgram(model)
	_, err := program.Run()
	return err
}
