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
	related    []*artifactv1.Artifact
	filtered   []*artifactv1.Artifact
	cursor     int
	offset     int // first visible row index (scroll offset)
	focused    bool
	searching  bool
	searchBuf  string
	width      int
	height     int
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
	visibleRows := p.height - 4
	if visibleRows < 1 {
		visibleRows = 1
	}
	switch msg.String() {
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
	titleStyle := styles.PaneTitle
	if p.focused {
		borderStyle = styles.FocusedBorder
		titleStyle = styles.PaneTitleFocused
	}

	// Collapsed: only show header row inside border.
	if p.height <= 3 {
		header := titleStyle.Render(fmt.Sprintf(" Related (%d) ", len(p.filtered)))
		return borderStyle.Width(p.width - 2).Height(1).Render(header)
	}

	title := "Related"
	if p.searching {
		title = "s " + p.searchBuf + "█"
	}

	header := titleStyle.Render(fmt.Sprintf(" %s (%d) ", title, len(p.filtered)))

	innerW := p.width - 4
	innerH := p.height - 4
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
	inner := lipgloss.JoinVertical(lipgloss.Left, header, body)
	return borderStyle.Width(p.width - 2).Height(p.height - 2).Render(inner)
}
