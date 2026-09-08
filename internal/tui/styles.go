package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// Lazygit-inspired palette
var (
	ColorActiveBorder   = lipgloss.Color("#50fa7b") // Green
	ColorInactiveBorder = lipgloss.Color("#44475a") // Slate
	ColorActiveTitle    = lipgloss.Color("#50fa7b")
	ColorInactiveTitle  = lipgloss.Color("#6272a4")
	ColorSelectedBg     = lipgloss.Color("#44475a")
	ColorSelectedFg     = lipgloss.Color("#ffffff")
	ColorDim            = lipgloss.Color("#6272a4")
	ColorSuccess        = lipgloss.Color("#50fa7b")
	ColorError          = lipgloss.Color("#ff5555")
	ColorWarning        = lipgloss.Color("#f1fa8c")
	ColorCyan           = lipgloss.Color("#8be9fd")
	ColorPurple         = lipgloss.Color("#bd93f9")
	ColorBgDark         = lipgloss.Color("#1e1f29")
)

// Base panel styling
func PanelStyle(active bool, width, height int) lipgloss.Style {
	borderColor := ColorInactiveBorder
	if active {
		borderColor = ColorActiveBorder
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(width).
		Height(height).
		Padding(0, 1)
}

// Title styling for panel headers
func PanelTitleStyle(active bool) lipgloss.Style {
	color := ColorInactiveTitle
	if active {
		color = ColorActiveTitle
	}
	return lipgloss.NewStyle().
		Foreground(color).
		Bold(active)
}

// Selected item in a list
var SelectedItemStyle = lipgloss.NewStyle().
	Background(ColorSelectedBg).
	Foreground(ColorSelectedFg).
	Bold(true)

// Normal item in a list
var NormalItemStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#f8f8f2"))

// Status indicators
var (
	StyleSuccess = lipgloss.NewStyle().Foreground(ColorSuccess).Bold(true)
	StyleError   = lipgloss.NewStyle().Foreground(ColorError).Bold(true)
	StyleWarning = lipgloss.NewStyle().Foreground(ColorWarning).Bold(true)
	StyleCyan    = lipgloss.NewStyle().Foreground(ColorCyan)
	StylePurple  = lipgloss.NewStyle().Foreground(ColorPurple).Bold(true)
	StyleDim     = lipgloss.NewStyle().Foreground(ColorDim)
)

// Status bar at bottom
var (
	StatusBarKeyStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#6272a4")).
				Foreground(lipgloss.Color("#f8f8f2")).
				Bold(true).
				Padding(0, 1)

	StatusBarDescStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#282a36")).
				Foreground(lipgloss.Color("#f8f8f2")).
				Padding(0, 1)

	StatusBarOnlineBadge = lipgloss.NewStyle().
				Background(ColorSuccess).
				Foreground(lipgloss.Color("#000000")).
				Bold(true).
				Padding(0, 1)

	StatusBarOfflineBadge = lipgloss.NewStyle().
				Background(ColorError).
				Foreground(lipgloss.Color("#ffffff")).
				Bold(true).
				Padding(0, 1)
)

// Modal popup dialog styling
var ModalStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorActiveBorder).
	Background(ColorBgDark).
	Padding(1, 2)
