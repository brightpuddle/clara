package panes

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	artifactv1 "github.com/brightpuddle/clara/gen/artifact/v1"
	"github.com/brightpuddle/clara/internal/artifact"
	"github.com/brightpuddle/clara/tui/styles"
)

// RelatedPane shows artifacts related to the currently selected artifact.
type RelatedPane struct {
	related   []*artifactv1.Artifact
	filtered  []*artifactv1.Artifact
	cursor    int
	offset    int // first visible row index (scroll offset)
	focused   bool
	searching bool
	searchBuf string
	lastKey   string // tracks multi-key sequences (e.g. "gg")
	width     int
	height    int
}

// NewRelatedPane creates an empty RelatedPane.
func NewRelatedPane() RelatedPane {
	return RelatedPane{}
}

// SetRelated replaces the related artifacts list.
func (p *RelatedPane) SetRelated(arts []*artifactv1.Artifact) {
	p.related = arts
	p.applyFilter()
}

// SetSize sets the pane dimensions.
func (p *RelatedPane) SetSize(w, h int) {
	p.width = w
	p.height = h
}

// SetFocused sets whether this pane has keyboard focus.
func (p *RelatedPane) SetFocused(f bool) {
	p.focused = f
	if !f {
		p.searching = false
		p.searchBuf = ""
		p.lastKey = ""
		p.applyFilter()
	}
}

// IsSearching returns true if the pane is in search mode.
func (p *RelatedPane) IsSearching() bool {
	return p.searching
}

// Selected returns the currently selected related artifact, or nil.
func (p *RelatedPane) Selected() *artifactv1.Artifact {
	if len(p.filtered) == 0 {
		return nil
	}
	return p.filtered[p.cursor]
}

// clampScroll adjusts offset to keep cursor visible with scrollPad context lines.
func (p *RelatedPane) clampScroll(visibleRows int) {
	if visibleRows <= 0 {
		return
	}
	if p.cursor >= p.offset+visibleRows-scrollPad {
		p.offset = p.cursor - visibleRows + scrollPad + 1
	}
	if p.cursor < p.offset+scrollPad {
		p.offset = p.cursor - scrollPad
	}
	if p.offset < 0 {
		p.offset = 0
	}
	max := len(p.filtered) - visibleRows
	if max < 0 {
		max = 0
	}
	if p.offset > max {
		p.offset = max
	}
}

// Update handles key events for the related pane.
func (p *RelatedPane) Update(msg tea.KeyMsg) string {
	if p.searching {
		return p.updateSearch(msg)
	}
	visibleRows := p.height - 3
	if visibleRows < 1 {
		visibleRows = 1
	}

	key := msg.String()

	// Handle "gg" sequence: first g sets lastKey, second g jumps to top.
	if key == "g" && p.lastKey == "g" {
		p.lastKey = ""
		p.cursor = 0
		p.offset = 0
		return ""
	}

	prev := p.lastKey
	p.lastKey = ""
	_ = prev

	switch key {
	case "g":
		p.lastKey = "g"
	case "G":
		if len(p.filtered) > 0 {
			p.cursor = len(p.filtered) - 1
			p.clampScroll(visibleRows)
		}
	case "j", "down":
		if p.cursor < len(p.filtered)-1 {
			p.cursor++
			p.clampScroll(visibleRows)
		}
	case "k", "up":
		if p.cursor > 0 {
			p.cursor--
			p.clampScroll(visibleRows)
		}
	case "s":
		p.searching = true
		p.searchBuf = ""
	case "enter":
		if sel := p.Selected(); sel != nil {
			return "edit:" + sel.Id
		}
	case "o":
		if sel := p.Selected(); sel != nil {
			return "open:" + sel.Id
		}
	}
	return ""
}

func (p *RelatedPane) updateSearch(msg tea.KeyMsg) string {
	switch msg.String() {
	case "esc", "ctrl+c":
		p.searching = false
		p.searchBuf = ""
		p.applyFilter()
	case "enter":
		p.searching = false
	case "backspace":
		if len(p.searchBuf) > 0 {
			p.searchBuf = p.searchBuf[:len(p.searchBuf)-1]
			p.applyFilter()
		}
	default:
		if len(msg.Runes) > 0 {
			p.searchBuf += string(msg.Runes)
			p.applyFilter()
		}
	}
	return ""
}

func (p *RelatedPane) applyFilter() {
	p.cursor = 0
	p.offset = 0
	if p.searchBuf == "" {
		p.filtered = p.related
		return
	}
	q := strings.ToLower(p.searchBuf)
	var out []*artifactv1.Artifact
	for _, a := range p.related {
		if strings.Contains(strings.ToLower(a.Title), q) {
			out = append(out, a)
		}
	}
	p.filtered = out
}

// View renders the related context pane.
func (p *RelatedPane) View() string {
	if p.width <= 0 || p.height <= 0 {
		return ""
	}

	borderStyle := styles.UnfocusedBorder
	if p.focused {
		borderStyle = styles.FocusedBorder
	}

	// Collapsed: show first related item preview in the single interior line.
	if p.height <= 3 {
		preview := ""
		if len(p.filtered) > 0 {
			first := p.filtered[0]
			icon := artifact.KindIcon(first.Kind)
			maxW := p.width - 6
			if maxW < 1 {
				maxW = 1
			}
			title := first.Title
			if len(title) > maxW {
				title = title[:maxW]
			}
			preview = icon + " " + title
		}
		rendered := borderStyle.Width(p.width - 2).Height(1).Render(preview)
		return styles.InjectBorderTitle(rendered, "2", fmt.Sprintf("Related (%d)", len(p.filtered)), p.width, p.focused)
	}

	title := "Related"
	if p.searching {
		title = "s " + p.searchBuf + "█"
	}

	titleStr := fmt.Sprintf("%s (%d)", title, len(p.filtered))

	innerW := p.width - 4
	innerH := p.height - 3
	if innerH < 1 {
		innerH = 1
	}
	if innerW < 1 {
		innerW = 1
	}

	var rows []string
	for i := p.offset; i < len(p.filtered) && i < p.offset+innerH; i++ {
		a := p.filtered[i]
		icon := artifact.KindIcon(a.Kind)
		color := kindColor(a.Kind)
		titleText := truncateStr(a.Title, innerW-4)

		selected := i == p.cursor && p.focused
		if selected {
			line := fmt.Sprintf("%s %s", icon, titleText)
			rows = append(rows, styles.ItemSelected.Width(innerW).Render(line))
		} else {
			iconStr := lipgloss.NewStyle().Foreground(color).Render(icon)
			line := fmt.Sprintf("%s %s", iconStr, titleText)
			rows = append(rows, styles.ItemNormal.Width(innerW).Render(line))
		}
	}

	if len(rows) == 0 {
		rows = append(rows, styles.Muted.Render("  no related items"))
	}

	body := strings.Join(rows, "\n")
	rendered := borderStyle.Width(p.width - 2).Height(p.height - 2).Render(body)
	return styles.InjectBorderTitle(rendered, "2", titleStr, p.width, p.focused)
}

// SelectAtRow selects the artifact at content row (0-indexed).
// Returns true if the selection changed.
func (p *RelatedPane) SelectAtRow(row int) bool {
	idx := p.offset + row
	if idx < 0 || idx >= len(p.filtered) {
		return false
	}
	if idx == p.cursor {
		return false
	}
	p.cursor = idx
	return true
}

// Offset returns the current scroll offset.
func (p *RelatedPane) Offset() int { return p.offset }

// ScrollDown moves the cursor down by one.
func (p *RelatedPane) ScrollDown() {
	if p.cursor < len(p.filtered)-1 {
		p.cursor++
		visibleRows := p.height - 3
		if visibleRows < 1 {
			visibleRows = 1
		}
		p.clampScroll(visibleRows)
	}
}

// ScrollUp moves the cursor up by one.
func (p *RelatedPane) ScrollUp() {
	if p.cursor > 0 {
		p.cursor--
		visibleRows := p.height - 3
		if visibleRows < 1 {
			visibleRows = 1
		}
		p.clampScroll(visibleRows)
	}
}
