// Package panes provides Bubbletea components for the Clara TUI.
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

// ArtifactsPane is the left-sidebar unified artifact list.
type ArtifactsPane struct {
	artifacts []*artifactv1.Artifact
	filtered  []*artifactv1.Artifact
	cursor    int
	focused   bool
	searching bool
	searchBuf string
	width     int
	height    int
}

// NewArtifactsPane creates an empty ArtifactsPane.
func NewArtifactsPane() ArtifactsPane {
	return ArtifactsPane{}
}

// SetArtifacts replaces the artifact list and resets the cursor.
func (p *ArtifactsPane) SetArtifacts(arts []*artifactv1.Artifact) {
	p.artifacts = arts
	p.applyFilter()
}

// SetSize sets the pane dimensions.
func (p *ArtifactsPane) SetSize(w, h int) {
	p.width = w
	p.height = h
}

// SetFocused sets whether this pane is focused.
func (p *ArtifactsPane) SetFocused(f bool) {
	p.focused = f
	if !f {
		p.searching = false
		p.searchBuf = ""
		p.applyFilter()
	}
}

// Selected returns the currently selected artifact, or nil.
func (p *ArtifactsPane) Selected() *artifactv1.Artifact {
	if len(p.filtered) == 0 {
		return nil
	}
	return p.filtered[p.cursor]
}

// Update handles key messages for this pane.
// Returns (updated, actionMsg) where actionMsg is non-empty for special actions.
func (p *ArtifactsPane) Update(msg tea.KeyMsg) (action string) {
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
	case "/":
		p.searching = true
		p.searchBuf = ""
	case " ":
		if sel := p.Selected(); sel != nil {
			return "done:" + sel.Id
		}
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

func (p *ArtifactsPane) updateSearch(msg tea.KeyMsg) string {
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

func (p *ArtifactsPane) applyFilter() {
	p.cursor = 0
	if p.searchBuf == "" {
		p.filtered = p.artifacts
		return
	}
	q := strings.ToLower(p.searchBuf)
	var out []*artifactv1.Artifact
	for _, a := range p.artifacts {
		if strings.Contains(strings.ToLower(a.Title), q) ||
			strings.Contains(strings.ToLower(a.Content), q) {
			out = append(out, a)
		}
	}
	p.filtered = out
}

// View renders the artifacts pane.
func (p *ArtifactsPane) View() string {
	if p.width <= 0 || p.height <= 0 {
		return ""
	}

	title := "Artifacts"
	if p.searching {
		title = "/ " + p.searchBuf + "█"
	}

	borderStyle := styles.UnfocusedBorder
	titleStyle := styles.PaneTitle
	if p.focused {
		borderStyle = styles.FocusedBorder
		titleStyle = styles.PaneTitleFocused
	}

	header := titleStyle.Render(fmt.Sprintf(" %s (%d) ", title, len(p.filtered)))

	innerH := p.height - 4 // account for border + header
	if innerH < 1 {
		innerH = 1
	}

	var rows []string
	for i, a := range p.filtered {
		if i >= innerH {
			break
		}
		icon := artifact.KindIcon(a.Kind)
		heatBar := styles.HeatBar(a.HeatScore)
		kindColor := kindColor(a.Kind)

		title := truncateStr(a.Title, p.width-12)
		line := fmt.Sprintf("%s %s %-*s %s",
			lipgloss.NewStyle().Foreground(kindColor).Render(icon),
			heatBar,
			p.width-12,
			title,
			"",
		)
		if i == p.cursor && p.focused {
			rows = append(rows, styles.ItemSelected.Width(p.width-4).Render(line))
		} else {
			rows = append(rows, styles.ItemNormal.Width(p.width-4).Render(line))
		}
	}

	if len(rows) == 0 {
		rows = append(rows, styles.Muted.Render("  no artifacts"))
	}

	body := strings.Join(rows, "\n")
	inner := lipgloss.JoinVertical(lipgloss.Left, header, body)
	return borderStyle.Width(p.width - 2).Height(p.height - 2).Render(inner)
}

func kindColor(kind artifactv1.ArtifactKind) lipgloss.Color {
	switch kind {
	case artifactv1.ArtifactKind_ARTIFACT_KIND_REMINDER:
		return styles.ColorReminder
	case artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE:
		return styles.ColorNote
	case artifactv1.ArtifactKind_ARTIFACT_KIND_FILE:
		return styles.ColorFile
	case artifactv1.ArtifactKind_ARTIFACT_KIND_EMAIL:
		return styles.ColorEmail
	case artifactv1.ArtifactKind_ARTIFACT_KIND_BOOKMARK:
		return styles.ColorBookmark
	case artifactv1.ArtifactKind_ARTIFACT_KIND_LOG:
		return styles.ColorLog
	default:
		return styles.ColorSuggestion
	}
}

func truncateStr(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}
