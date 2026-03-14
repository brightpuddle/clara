package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

const ansiReset = "\x1b[0m"

type Theme struct {
	DimmedTrueColor string
	MagentaCode     int
	CyanCode        int
	BlueCode        int
	GreenCode       int
	YellowCode      int
}

func DetectTheme() Theme {
	fg := termenv.ConvertToRGB(termenv.ForegroundColor())
	return Theme{
		DimmedTrueColor: dimmedHexFromRGB(
			uint8(fg.R*255),
			uint8(fg.G*255),
			uint8(fg.B*255),
			lipgloss.HasDarkBackground(),
		),
		MagentaCode: 35,
		CyanCode:    36,
		BlueCode:    34,
		GreenCode:   32,
		YellowCode:  33,
	}
}

func dimmedHexFromRGB(r, g, b uint8, darkBackground bool) string {
	scale := 0.72
	if !darkBackground {
		scale = 0.6
	}
	return fmt.Sprintf(
		"#%02x%02x%02x",
		uint8(float64(r)*scale),
		uint8(float64(g)*scale),
		uint8(float64(b)*scale),
	)
}

func (t Theme) ansi(code int, text string) string {
	if text == "" {
		return ""
	}
	return fmt.Sprintf("\x1b[%dm%s%s", code, text, ansiReset)
}

func (t Theme) trueColor(hex, text string) string {
	if text == "" {
		return ""
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(hex))
	return style.Render(text)
}

func (t Theme) Dimmed(text string) string  { return t.trueColor(t.DimmedTrueColor, text) }
func (t Theme) Magenta(text string) string { return t.ansi(t.MagentaCode, text) }
func (t Theme) Cyan(text string) string    { return t.ansi(t.CyanCode, text) }
func (t Theme) Blue(text string) string    { return t.ansi(t.BlueCode, text) }
func (t Theme) Green(text string) string   { return t.ansi(t.GreenCode, text) }
func (t Theme) Yellow(text string) string  { return t.ansi(t.YellowCode, text) }

func horizontalRule(width int) string {
	if width <= 0 {
		width = 80
	}
	return strings.Repeat("─", width)
}
