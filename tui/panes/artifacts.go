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
	offset    int // first visible row index (scroll offset)
	focused   bool
	searching bool
	searchBuf string
	lastKey   string // tracks multi-key sequences (e.g. "gg")
	width     int
	height    int
}

// NewArtifactsPane creates an empty ArtifactsPane.
func NewArtifactsPane() ArtifactsPane {
	return ArtifactsPane{}
}

// SetArtifacts replaces the artifact list, preserving the cursor on the previously
// selected artifact if it still exists in the new list.
func (p *ArtifactsPane) SetArtifacts(arts []*artifactv1.Artifact) {
	selectedID := ""
	if sel := p.Selected(); sel != nil {
		selectedID = sel.Id
	}
	p.artifacts = arts
	p.applyFilter()
	// Restore cursor to the previously selected artifact if it still exists.
	if selectedID != "" {
		for i, a := range p.filtered {
			if a.Id == selectedID {
				p.cursor = i
				visibleRows := p.height - 3
				if visibleRows < 1 {
					visibleRows = 1
				}
				p.clampScroll(visibleRows)
				break
			}
		}
	}
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
		p.lastKey = ""
		p.applyFilter()
	}
}

// IsSearching returns true if the pane is in search mode.
func (p *ArtifactsPane) IsSearching() bool {
	return p.searching
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

	// visibleRows is needed to clamp scroll after cursor movement. Compute it
	// from current height (same formula as View uses: height - 3).
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

	// If the previous key was "g" but this key is not "g", the sequence is
	// abandoned — fall through to normal handling with prev discarded.
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

// scrollPad is the number of lines kept visible above/below the cursor when
// scrolling (i.e. the cursor starts scrolling when it's this close to the edge).
const scrollPad = 3

// clampScroll adjusts the scroll offset so the cursor is always visible with
// scrollPad lines of context, unless there are fewer items than the viewport.
func (p *ArtifactsPane) clampScroll(visibleRows int) {
	if visibleRows <= 0 {
		return
	}
	// Scroll down: cursor is too close to the bottom of the viewport.
	if p.cursor >= p.offset+visibleRows-scrollPad {
		p.offset = p.cursor - visibleRows + scrollPad + 1
	}
	// Scroll up: cursor is too close to the top of the viewport.
	if p.cursor < p.offset+scrollPad {
		p.offset = p.cursor - scrollPad
	}
	// Clamp offset to valid range.
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

func (p *ArtifactsPane) applyFilter() {
	p.cursor = 0
	p.offset = 0
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

	borderStyle := styles.UnfocusedBorder
	if p.focused {
		borderStyle = styles.FocusedBorder
	}

	// Collapsed: only show header row inside border.
	if p.height <= 3 {
		rendered := borderStyle.Width(p.width - 2).Height(1).Render("")
		return styles.InjectBorderTitle(rendered, "1", fmt.Sprintf("Artifacts (%d)", len(p.filtered)), p.width, p.focused)
	}

	title := "Artifacts"
	if p.searching {
		title = "/ " + p.searchBuf + "█"
	}

	titleStr := fmt.Sprintf("%s (%d)", title, len(p.filtered))

	innerW := p.width - 4 // account for border+padding
	innerH := p.height - 3 // account for border (no header row)
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
		kindCol := kindColor(a.Kind)
		titleText := truncateStr(a.Title, innerW-4) // icon(1) + space(1) + title + margin

		selected := i == p.cursor && p.focused
		if selected {
			// Plain text for selected row — single unified style avoids broken
			// background from nested ANSI resets.
			line := fmt.Sprintf("%s %s", icon, titleText)
			rows = append(rows, styles.ItemSelected.Width(innerW).Render(line))
		} else {
			iconStr := lipgloss.NewStyle().Foreground(kindCol).Render(icon)
			line := fmt.Sprintf("%s %s", iconStr, titleText)
			rows = append(rows, styles.ItemNormal.Width(innerW).Render(line))
		}
	}

	if len(rows) == 0 {
		rows = append(rows, styles.Muted.Render("  no artifacts"))
	}

	body := strings.Join(rows, "\n")
	rendered := borderStyle.Width(p.width - 2).Height(p.height - 2).Render(body)
	return styles.InjectBorderTitle(rendered, "1", titleStr, p.width, p.focused)
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

// SelectAtRow selects the artifact at content row (0-indexed, relative to pane top border).
// Returns true if the selection changed.
func (p *ArtifactsPane) SelectAtRow(row int) bool {
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
func (p *ArtifactsPane) Offset() int { return p.offset }

// ScrollDown moves the cursor down by one.
func (p *ArtifactsPane) ScrollDown() {
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
func (p *ArtifactsPane) ScrollUp() {
	if p.cursor > 0 {
		p.cursor--
		visibleRows := p.height - 3
		if visibleRows < 1 {
			visibleRows = 1
		}
		p.clampScroll(visibleRows)
	}
}
