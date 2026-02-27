package main

import (
	"fmt"
	"os"

	"github.com/brightpuddle/clara/client/tui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	serverURL := envOr("CLARA_SERVER_URL", "http://localhost:8080")

	apiClient := tui.NewAPIClient(serverURL)
	model := tui.New(apiClient)

	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
