package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/brightpuddle/clara/client/tui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	cfg := loadClientConfig()
	apiClient := tui.NewAPIClient(cfg.Server.URL)

	// Non-interactive mode: run a command and print output.
	if len(os.Args) > 1 {
		line := strings.Join(os.Args[1:], " ")

		// Treat --help / -h as an alias for the help command.
		if line == "--help" || line == "-h" {
			line = "help"
		}

		out, err := tui.RunCommand(line, apiClient)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		if out != "" {
			fmt.Println(out)
		}
		return
	}

	// Interactive TUI mode.
	model := tui.New(apiClient)
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
