package panes

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/brightpuddle/clara/tui/styles"
)

// SettingsCategory represents a named settings section.
type SettingsCategory struct {
	ID    string
	Label string
}

var DefaultCategories = []SettingsCategory{
	{ID: "status", Label: "Status"},
	{ID: "tui", Label: "TUI"},
	{ID: "integrations", Label: "Integrations"},
}

// SettingsPane is the left sidebar settings navigator.
type SettingsPane struct {
	categories []SettingsCategory
	cursor     int
	focused    bool
	width      int
	height     int
}

func NewSettingsPane() SettingsPane {
	return SettingsPane{categories: DefaultCategories}
}

func (p *SettingsPane) SetSize(w, h int)  { p.width = w; p.height = h }
func (p *SettingsPane) SetFocused(f bool) { p.focused = f }

func (p *SettingsPane) Selected() *SettingsCategory {
	if len(p.categories) == 0 {
		return nil
	}
	return &p.categories[p.cursor]
}

// Update handles key events. Returns action string ("settings:status", etc.)
func (p *SettingsPane) Update(msg tea.KeyMsg) string {
	switch msg.String() {
	case "j", "down":
		if p.cursor < len(p.categories)-1 {
			p.cursor++
		}
	case "k", "up":
		if p.cursor > 0 {
			p.cursor--
		}
	case "enter":
		if sel := p.Selected(); sel != nil {
			return "settings:" + sel.ID
		}
	}
	return ""
}

func (p *SettingsPane) View() string {
	if p.width <= 0 || p.height <= 0 {
		return ""
	}

	borderStyle := styles.UnfocusedBorder
	titleStyle := styles.PaneTitle
	if p.focused {
		borderStyle = styles.FocusedBorder
		titleStyle = styles.PaneTitleFocused
	}

	header := titleStyle.Render(fmt.Sprintf(" Settings (%d) ", len(p.categories)))

	if p.height <= 3 {
		return borderStyle.Width(p.width - 2).Height(1).Render(header)
	}

	innerW := p.width - 4
	innerH := p.height - 4
	if innerH < 1 {
		innerH = 1
	}
	if innerW < 1 {
		innerW = 1
	}

	var rows []string
	for i, cat := range p.categories {
		if i >= innerH {
			break
		}
		label := truncateStr(cat.Label, innerW-2)
		if i == p.cursor && p.focused {
			rows = append(rows, styles.ItemSelected.Width(innerW).Render("⚙ "+label))
		} else {
			rows = append(rows, styles.ItemNormal.Width(innerW).Render("⚙ "+label))
		}
	}

	if len(rows) == 0 {
		rows = append(rows, styles.Muted.Render("  no settings"))
	}

	body := strings.Join(rows, "\n")
	inner := lipgloss.JoinVertical(lipgloss.Left, header, body)
	return borderStyle.Width(p.width - 2).Height(p.height - 2).Render(inner)
}
