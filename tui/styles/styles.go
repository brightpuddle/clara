// Package styles provides the lipgloss theme and icon definitions for the Clara TUI.
package styles

import "github.com/charmbracelet/lipgloss"

// Color palette.
var (
	ColorBg         = lipgloss.Color("#1e1e2e")
	ColorFg         = lipgloss.Color("#cdd6f4")
	ColorMuted      = lipgloss.Color("#585b70")
	ColorBorder     = lipgloss.Color("#313244")
	ColorBorderFocus = lipgloss.Color("#89b4fa")

	// Heat gradient: green → yellow → orange → red.
	ColorHeatLow    = lipgloss.Color("#a6e3a1") // green
	ColorHeatMed    = lipgloss.Color("#f9e2af") // yellow
	ColorHeatHigh   = lipgloss.Color("#fab387") // orange
	ColorHeatUrgent = lipgloss.Color("#f38ba8") // red

	// Artifact kind colors.
	ColorReminder  = lipgloss.Color("#f38ba8") // red
	ColorNote      = lipgloss.Color("#a6e3a1") // green
	ColorFile      = lipgloss.Color("#89b4fa") // blue
	ColorEmail     = lipgloss.Color("#cba6f7") // purple
	ColorBookmark  = lipgloss.Color("#f9e2af") // yellow
	ColorLog       = lipgloss.Color("#fab387") // orange
	ColorSuggestion = lipgloss.Color("#94e2d5") // teal
)

// Pane border styles.
var (
	FocusedBorder = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorderFocus).
		Padding(0, 1)

	UnfocusedBorder = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorder).
		Padding(0, 1)

	// PaneTitle is used for collapsed pane headers.
	PaneTitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorMuted).
		Padding(0, 1)

	PaneTitleFocused = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorBorderFocus).
		Padding(0, 1)

	// Item styles.
	ItemSelected = lipgloss.NewStyle().
		Background(lipgloss.Color("#313244")).
		Foreground(ColorFg).
		Bold(true)

	ItemNormal = lipgloss.NewStyle().
		Foreground(ColorFg)

	Muted = lipgloss.NewStyle().Foreground(ColorMuted)

	Bold = lipgloss.NewStyle().Bold(true)

	// Search prompt.
	SearchPrompt = lipgloss.NewStyle().
		Foreground(ColorBorderFocus).
		Bold(true)
)

// HeatColor returns the appropriate color for a heat score in [0, 1].
func HeatColor(score float64) lipgloss.Color {
	switch {
	case score >= 0.8:
		return ColorHeatUrgent
	case score >= 0.6:
		return ColorHeatHigh
	case score >= 0.35:
		return ColorHeatMed
	default:
		return ColorHeatLow
	}
}

// HeatBar returns a 3-char heat indicator.
func HeatBar(score float64) string {
	color := HeatColor(score)
	var bar string
	switch {
	case score >= 0.8:
		bar = "▰▰▰"
	case score >= 0.6:
		bar = "▰▰▱"
	case score >= 0.35:
		bar = "▰▱▱"
	default:
		bar = "▱▱▱"
	}
	return lipgloss.NewStyle().Foreground(color).Render(bar)
}
