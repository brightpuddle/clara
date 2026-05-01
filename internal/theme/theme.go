// Package theme provides terminal colour-styling helpers used by Clara's CLI
// output formatters.
package theme

import (
	"encoding/json"
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// Theme holds the colour palette and base styles used by CLI output formatters.
type Theme struct {
	Primary   lipgloss.Color
	Secondary lipgloss.Color
	Highlight lipgloss.Color
	Text      lipgloss.Color
	Dim       lipgloss.Color
	Error     lipgloss.Color
	Success   lipgloss.Color

	MagentaColor lipgloss.Color
	YellowColor  lipgloss.Color

	BaseStyle    lipgloss.Style
	PromptStyle  lipgloss.Style
	ActiveItem   lipgloss.Style
	InactiveItem lipgloss.Style
	TitleStyle   lipgloss.Style
	DimStyle     lipgloss.Style
}

// DefaultTheme returns a Theme populated with the standard ANSI colour palette.
func DefaultTheme() *Theme {
	t := &Theme{
		Primary:      lipgloss.Color("12"), // ANSI Blue
		Secondary:    lipgloss.Color("6"),  // ANSI Cyan
		Highlight:    lipgloss.Color("5"),  // ANSI Magenta
		Text:         lipgloss.Color("7"),  // ANSI White
		Dim:          lipgloss.Color("8"),  // ANSI Gray
		Error:        lipgloss.Color("9"),  // ANSI Red
		Success:      lipgloss.Color("10"), // ANSI Green
		MagentaColor: lipgloss.Color("5"),  // ANSI Magenta
		YellowColor:  lipgloss.Color("3"),  // ANSI Yellow
	}

	t.DimStyle = lipgloss.NewStyle().Foreground(t.Dim)
	t.BaseStyle = lipgloss.NewStyle().Foreground(t.Text)

	t.PromptStyle = lipgloss.NewStyle().
		BorderTop(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(t.Dim).
		Padding(0, 1)

	t.ActiveItem = lipgloss.NewStyle().
		BorderLeft(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(t.Highlight).
		Padding(0, 1)

	t.InactiveItem = lipgloss.NewStyle().
		BorderLeft(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(t.Dim).
		Padding(0, 1)

	t.TitleStyle = lipgloss.NewStyle().
		Foreground(t.Secondary).
		Bold(true).
		PaddingBottom(1)

	return t
}

// DetectTheme returns the default theme. In the future this may probe the
// terminal for dark/light mode and return an appropriate palette.
func DetectTheme() Theme {
	return *DefaultTheme()
}

// Dimmed renders s in the theme's dim colour.
func (t Theme) Dimmed(s string) string {
	return t.DimStyle.Render(s)
}

// Cyan renders s in the theme's secondary (cyan) colour.
func (t Theme) Cyan(s string) string {
	return lipgloss.NewStyle().Foreground(t.Secondary).Render(s)
}

// Magenta renders s in the theme's magenta colour.
func (t Theme) Magenta(s string) string {
	return lipgloss.NewStyle().Foreground(t.MagentaColor).Render(s)
}

// Yellow renders s in the theme's yellow colour.
func (t Theme) Yellow(s string) string {
	return lipgloss.NewStyle().Foreground(t.YellowColor).Render(s)
}

// Green renders s in the theme's success (green) colour.
func (t Theme) Green(s string) string {
	return lipgloss.NewStyle().Foreground(t.Success).Render(s)
}

// Red renders s in the theme's error (red) colour.
func (t Theme) Red(s string) string {
	return lipgloss.NewStyle().Foreground(t.Error).Render(s)
}

// RenderJSON pretty-prints v as indented JSON, falling back to fmt.Sprintf on
// marshal error.
func RenderJSON(t Theme, v any) string {
	if v == nil {
		return t.Dimmed("null")
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
