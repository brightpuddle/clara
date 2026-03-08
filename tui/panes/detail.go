package panes

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	artifactv1 "github.com/brightpuddle/clara/gen/artifact/v1"
	"github.com/brightpuddle/clara/internal/artifact"
	"github.com/brightpuddle/clara/tui/styles"
)

// DetailPane renders the preview and metadata for the selected artifact.
type DetailPane struct {
	artifact *artifactv1.Artifact
	focused  bool
	width    int
	height   int
	scrollY  int
}

// NewDetailPane creates an empty DetailPane.
func NewDetailPane() DetailPane {
	return DetailPane{}
}

// SetArtifact sets the artifact to display.
func (p *DetailPane) SetArtifact(a *artifactv1.Artifact) {
	p.artifact = a
	p.scrollY = 0
}

// SetSize sets the pane dimensions.
func (p *DetailPane) SetSize(w, h int) {
	p.width = w
	p.height = h
}

// SetFocused sets whether this pane has focus.
func (p *DetailPane) SetFocused(f bool) {
	p.focused = f
}

// ScrollDown scrolls the detail view down.
func (p *DetailPane) ScrollDown() {
	p.scrollY++
}

// ScrollUp scrolls the detail view up.
func (p *DetailPane) ScrollUp() {
	if p.scrollY > 0 {
		p.scrollY--
	}
}

// View renders the detail pane.
func (p *DetailPane) View() string {
	if p.width <= 0 || p.height <= 0 {
		return ""
	}

	borderStyle := styles.UnfocusedBorder
	titleStyle := styles.PaneTitle
	if p.focused {
		borderStyle = styles.FocusedBorder
		titleStyle = styles.PaneTitleFocused
	}

	header := titleStyle.Render(" Detail ")

	if p.artifact == nil {
		empty := styles.Muted.Render("  Select an artifact to preview")
		inner := lipgloss.JoinVertical(lipgloss.Left, header, empty)
		return borderStyle.Width(p.width - 2).Height(p.height - 2).Render(inner)
	}

	a := p.artifact
	icon := artifact.KindIcon(a.Kind)
	color := kindColor(a.Kind)

	// Metadata section.
	titleLine := lipgloss.NewStyle().Foreground(color).Bold(true).
		Render(fmt.Sprintf("%s  %s", icon, a.Title))

	var meta []string
	meta = append(meta, styles.ItemNormal.Render(fmt.Sprintf("kind:   %s", kindName(a.Kind))))
	meta = append(meta, styles.ItemNormal.Render(fmt.Sprintf("heat:   %s %.2f", styles.HeatBar(a.HeatScore), a.HeatScore)))
	if a.SourcePath != "" {
		meta = append(meta, styles.ItemNormal.Render(fmt.Sprintf("source: %s", truncateStr(a.SourcePath, p.width-12))))
	}
	if a.SourceApp != "" {
		meta = append(meta, styles.ItemNormal.Render(fmt.Sprintf("app:    %s", a.SourceApp)))
	}
	if a.DueAt != nil {
		meta = append(meta, styles.ItemNormal.Render(fmt.Sprintf("due:    %s", a.DueAt.AsTime().Format("2006-01-02 15:04"))))
	}
	if len(a.Tags) > 0 {
		meta = append(meta, styles.ItemNormal.Render(fmt.Sprintf("tags:   %s", strings.Join(a.Tags, ", "))))
	}
	if a.Kind == artifactv1.ArtifactKind_ARTIFACT_KIND_REMINDER {
		if list, ok := a.Metadata["list"]; ok && list != "" {
			meta = append(meta, styles.ItemNormal.Render(fmt.Sprintf("list:   %s", list)))
		}
		if pri, ok := a.Metadata["priority"]; ok {
			priNames := map[string]string{"0": "none", "1": "high", "5": "medium", "9": "low"}
			if name, ok := priNames[pri]; ok {
				meta = append(meta, styles.ItemNormal.Render(fmt.Sprintf("priority: %s", name)))
			} else {
				meta = append(meta, styles.ItemNormal.Render(fmt.Sprintf("priority: %s", pri)))
			}
		}
	}

	separator := strings.Repeat("─", p.width-4)

	// Content section with scrolling.
	contentLines := strings.Split(a.Content, "\n")
	innerH := p.height - 4 - len(meta) - 3 // header + meta + separators
	if innerH < 1 {
		innerH = 1
	}

	start := p.scrollY
	if start >= len(contentLines) {
		start = max(0, len(contentLines)-1)
	}
	end := start + innerH
	if end > len(contentLines) {
		end = len(contentLines)
	}

	var contentRows []string
	for _, line := range contentLines[start:end] {
		contentRows = append(contentRows, styles.ItemNormal.Width(p.width-4).Render(truncateStr(line, p.width-4)))
	}

	parts := []string{header, titleLine, ""}
	parts = append(parts, meta...)
	parts = append(parts, styles.Muted.Render(separator))
	parts = append(parts, contentRows...)

	inner := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return borderStyle.Width(p.width - 2).Height(p.height - 2).Render(inner)
}

func kindName(kind artifactv1.ArtifactKind) string {
	switch kind {
	case artifactv1.ArtifactKind_ARTIFACT_KIND_REMINDER:
		return "reminder"
	case artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE:
		return "note"
	case artifactv1.ArtifactKind_ARTIFACT_KIND_FILE:
		return "file"
	case artifactv1.ArtifactKind_ARTIFACT_KIND_EMAIL:
		return "email"
	case artifactv1.ArtifactKind_ARTIFACT_KIND_BOOKMARK:
		return "bookmark"
	case artifactv1.ArtifactKind_ARTIFACT_KIND_LOG:
		return "log"
	case artifactv1.ArtifactKind_ARTIFACT_KIND_SUGGESTION:
		return "suggestion"
	default:
		return "unknown"
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
