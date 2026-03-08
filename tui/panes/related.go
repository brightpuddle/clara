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

// Selected returns the currently selected related artifact, or nil.
func (p *RelatedPane) Selected() *artifactv1.Artifact {
	if len(p.filtered) == 0 {
		return nil
	}
	return p.filtered[p.cursor]
}

// Update handles key events for the related pane.
func (p *RelatedPane) Update(msg tea.KeyMsg) string {
	if p.searching {
		return p.updateSearch(msg)
	}
	switch msg.String() {
	case "j", "down":
		if p.cursor < len(p.filtered)-1 {
			p.cursor++
		}
	case "k", "up":
		if p.cursor > 0 {
			p.cursor--
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

	title := "Related"
	if p.searching {
		title = "s " + p.searchBuf + "█"
	}

	borderStyle := styles.UnfocusedBorder
	titleStyle := styles.PaneTitle
	if p.focused {
		borderStyle = styles.FocusedBorder
		titleStyle = styles.PaneTitleFocused
	}

	header := titleStyle.Render(fmt.Sprintf(" %s (%d) ", title, len(p.filtered)))

	innerH := p.height - 4
	if innerH < 1 {
		innerH = 1
	}

	var rows []string
	for i, a := range p.filtered {
		if i >= innerH {
			break
		}
		icon := artifact.KindIcon(a.Kind)
		color := kindColor(a.Kind)
		titleText := truncateStr(a.Title, p.width-8)

		selected := i == p.cursor && p.focused
		if selected {
			line := fmt.Sprintf("%s %s", icon, titleText)
			rows = append(rows, styles.ItemSelected.Width(p.width-4).Render(line))
		} else {
			iconStr := lipgloss.NewStyle().Foreground(color).Render(icon)
			line := fmt.Sprintf("%s %s", iconStr, titleText)
			rows = append(rows, styles.ItemNormal.Width(p.width-4).Render(line))
		}
	}

	if len(rows) == 0 {
		rows = append(rows, styles.Muted.Render("  no related items"))
	}

	body := strings.Join(rows, "\n")
	inner := lipgloss.JoinVertical(lipgloss.Left, header, body)
	return borderStyle.Width(p.width - 2).Height(p.height - 2).Render(inner)
}
