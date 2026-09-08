package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Run starts the interactive Clara TUI.
func Run(socketPath, cfgPath string) error {
	m := NewModel(socketPath, cfgPath)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
