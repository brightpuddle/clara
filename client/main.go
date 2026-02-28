package main

import (
	"fmt"
	"os"

	"github.com/brightpuddle/clara/client/tui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	cfg := loadClientConfig()

	apiClient := tui.NewAPIClient(cfg.Server.URL)
	model := tui.New(apiClient)

	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
