package panes

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/brightpuddle/clara/tui/styles"
)

// SettingsCategory represents a named settings section.
type SettingsCategory struct {
	ID    string
	Label string
}

var DefaultCategories = []SettingsCategory{
	{ID: "status", Label: "Status"},
	{ID: "config", Label: "Config"},
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

// Update handles key events.
// Returns "settings:nav:ID" on cursor movement, "settings:edit:ID" on Enter.
func (p *SettingsPane) Update(msg tea.KeyMsg) string {
	switch msg.String() {
	case "j", "down":
		if p.cursor < len(p.categories)-1 {
			p.cursor++
		}
		if sel := p.Selected(); sel != nil {
			return "settings:nav:" + sel.ID
		}
	case "k", "up":
		if p.cursor > 0 {
			p.cursor--
		}
		if sel := p.Selected(); sel != nil {
			return "settings:nav:" + sel.ID
		}
	case "enter":
		if sel := p.Selected(); sel != nil {
			return "settings:edit:" + sel.ID
		}
	}
	return ""
}

func (p *SettingsPane) View() string {
	if p.width <= 0 || p.height <= 0 {
		return ""
	}

	borderStyle := styles.UnfocusedBorder
	if p.focused {
		borderStyle = styles.FocusedBorder
	}

	if p.height <= 3 {
		preview := ""
		if len(p.categories) > 0 {
			maxW := p.width - 6
			if maxW < 1 {
				maxW = 1
			}
			label := p.categories[0].Label
			if len(label) > maxW {
				label = label[:maxW]
			}
			preview = "⚙ " + label
		}
		rendered := borderStyle.Width(p.width - 2).Height(1).Render(preview)
		return styles.InjectBorderTitle(rendered, "3", fmt.Sprintf("Settings (%d)", len(p.categories)), p.width, p.focused)
	}

	innerW := p.width - 4
	innerH := p.height - 3
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
	rendered := borderStyle.Width(p.width - 2).Height(p.height - 2).Render(body)
	return styles.InjectBorderTitle(rendered, "3", fmt.Sprintf("Settings (%d)", len(p.categories)), p.width, p.focused)
}

// SelectAtRow selects the settings category at the given row index (0-indexed).
// Returns true if the selection changed.
func (p *SettingsPane) SelectAtRow(row int) bool {
	if row < 0 || row >= len(p.categories) {
		return false
	}
	if row == p.cursor {
		return false
	}
	p.cursor = row
	return true
}

// ScrollDown moves the cursor down by one.
func (p *SettingsPane) ScrollDown() {
	if p.cursor < len(p.categories)-1 {
		p.cursor++
	}
}

// ScrollUp moves the cursor up by one.
func (p *SettingsPane) ScrollUp() {
	if p.cursor > 0 {
		p.cursor--
	}
}
