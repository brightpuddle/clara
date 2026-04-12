package tui

import (
	"encoding/json"
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

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

func DefaultTheme() *Theme {
	t := &Theme{
		Primary:   lipgloss.Color("12"), // ANSI Blue
		Secondary: lipgloss.Color("6"),  // ANSI Cyan
		Highlight: lipgloss.Color("5"),  // ANSI Magenta
		Text:      lipgloss.Color("7"),  // ANSI White
		Dim:       lipgloss.Color("8"),  // ANSI Gray
		Error:     lipgloss.Color("9"),  // ANSI Red
		Success:   lipgloss.Color("10"), // ANSI Green
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

func DetectTheme() Theme {
	return *DefaultTheme()
}

func (t Theme) Dimmed(s string) string {
	return t.DimStyle.Render(s)
}

func (t Theme) Cyan(s string) string {
	return lipgloss.NewStyle().Foreground(t.Secondary).Render(s)
}

func (t Theme) Magenta(s string) string {
	return lipgloss.NewStyle().Foreground(t.MagentaColor).Render(s)
}

func (t Theme) Yellow(s string) string {
	return lipgloss.NewStyle().Foreground(t.YellowColor).Render(s)
}

func (t Theme) Green(s string) string {
	return lipgloss.NewStyle().Foreground(t.Success).Render(s)
}

func (t Theme) Red(s string) string {
	return lipgloss.NewStyle().Foreground(t.Error).Render(s)
}

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
