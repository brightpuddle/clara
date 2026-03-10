package panes

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/brightpuddle/clara/tui/styles"
)

// HelpSection is a group of related shortcuts.
type HelpSection struct {
	Title    string
	Bindings [][2]string // [key, description] pairs
}

var helpSections = []HelpSection{
	{
		Title: "Navigation",
		Bindings: [][2]string{
			{"j / ↓", "Move down"},
			{"k / ↑", "Move up"},
			{"h / Shift+Tab", "Previous pane"},
			{"l / Tab", "Next pane"},
			{"1", "Focus Artifacts"},
			{"2", "Focus Related"},
			{"3", "Focus Settings"},
			{"0", "Focus Detail"},
			{"gg", "Go to top"},
			{"G", "Go to bottom"},
		},
	},
	{
		Title: "List Actions",
		Bindings: [][2]string{
			{"Space", "Mark item done"},
			{"Enter", "Open in editor"},
			{"o", "Open in native app"},
			{"/", "Search artifacts"},
			{"s", "Search current pane"},
			{"Esc", "Clear search"},
		},
	},
	{
		Title: "Detail View",
		Bindings: [][2]string{
			{"j / k", "Scroll down / up"},
			{"gg / G", "Top / bottom"},
			{"/", "Search in content"},
			{"n / N", "Next / prev match"},
		},
	},
	{
		Title: "Application",
		Bindings: [][2]string{
			{"?", "Toggle this help"},
			{"q", "Quit"},
		},
	},
}

// RenderHelp renders a centered help overlay for the given terminal dimensions.
func RenderHelp(width, height int) string {
	keyStyle := lipgloss.NewStyle().
		Foreground(styles.ColorBorderActive).
		Width(16).
		Align(lipgloss.Right)
	descStyle := lipgloss.NewStyle().
		Foreground(styles.ColorFg)
	titleStyle := lipgloss.NewStyle().
		Foreground(styles.ColorBorderActive).
		Bold(true)
	sepStyle := lipgloss.NewStyle().
		Foreground(styles.ColorMuted)

	var sb strings.Builder
	for i, sec := range helpSections {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(titleStyle.Render(sec.Title))
		sb.WriteString("\n")
		sb.WriteString(sepStyle.Render(strings.Repeat("─", 30)))
		sb.WriteString("\n")
		for _, b := range sec.Bindings {
			row := keyStyle.Render(b[0]) + "  " + descStyle.Render(b[1])
			sb.WriteString(row)
			sb.WriteString("\n")
		}
	}

	inner := strings.TrimRight(sb.String(), "\n")
	contentW := 52

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorBorderActive).
		Padding(1, 3).
		Width(contentW).
		Background(styles.ColorBg).
		Foreground(styles.ColorFg).
		Render(inner)

	boxW := lipgloss.Width(box)
	boxH := lipgloss.Height(box)

	leftPad := (width - boxW) / 2
	topPad := (height - boxH) / 2
	if leftPad < 0 {
		leftPad = 0
	}
	if topPad < 0 {
		topPad = 0
	}

	lines := strings.Split(box, "\n")
	blank := strings.Repeat(" ", width)
	result := strings.Repeat(blank+"\n", topPad)
	for _, line := range lines {
		result += strings.Repeat(" ", leftPad) + line + "\n"
	}
	return result
}
