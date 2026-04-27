package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ContentItem struct {
	ID           int64
	RunID        string
	IntentID     string
	Type         string
	Text         string
	Options      []string
	ResponseChan chan string
}

type contentModel struct {
	theme  *Theme
	width  int
	height int

	items        []ContentItem
	pendingItems []ContentItem

	scrollOffset  int
	selectedIndex int
	stickyBottom  bool
}

func newContentModel(theme *Theme) *contentModel {
	return &contentModel{
		theme: theme,
		items: []ContentItem{
			{Type: "notification", Text: "System online. Waiting for intents..."},
		},
		selectedIndex: -1,
		stickyBottom:  true,
	}
}

func (m *contentModel) Init() tea.Cmd {
	return nil
}

func (m *contentModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		switch msg.Type {
		case tea.MouseWheelUp:
			if m.scrollOffset > 0 {
				m.scrollOffset--
				m.stickyBottom = false
			}
		case tea.MouseWheelDown:
			m.scrollOffset++
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.selectedIndex > 0 {
				m.selectedIndex--
			} else if m.scrollOffset > 0 {
				m.scrollOffset--
				m.stickyBottom = false
			}
		case "down", "j":
			if m.selectedIndex < len(m.items)-1 {
				m.selectedIndex++
			} else {
				m.scrollOffset++
			}
		case "tab":
			m.selectedIndex++
			if m.selectedIndex >= len(m.items) {
				m.selectedIndex = 0
			}
		case "shift+tab":
			m.selectedIndex--
			if m.selectedIndex < 0 {
				m.selectedIndex = len(m.items) - 1
			}
		case "ctrl+d":
			m.scrollOffset += m.height / 2
		case "ctrl+u":
			m.scrollOffset -= m.height / 2
			if m.scrollOffset < 0 {
				m.scrollOffset = 0
			}
			m.stickyBottom = false
		}
	}
	return m, nil
}

func (m *contentModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *contentModel) addItem(item ContentItem) {
	// Deduplicate by ID
	if item.ID != 0 {
		for i := range m.items {
			if m.items[i].ID == item.ID {
				// If we have an active QA, don't overwrite it with a notification
				// that might have come from history load (which marks answered QAs as notifications).
				if m.items[i].Type == "qa" && item.Type == "notification" {
					return
				}
				m.items[i] = item
				return
			}
		}
		for i := range m.pendingItems {
			if m.pendingItems[i].ID == item.ID {
				m.pendingItems[i] = item
				return
			}
		}
	}

	if m.hasActiveQA() {
		m.pendingItems = append(m.pendingItems, item)
		return
	}
	m.items = append(m.items, item)
}

func (m *contentModel) hasActiveQA() bool {
	for i := len(m.items) - 1; i >= 0; i-- {
		if m.items[i].Type == "qa" {
			return true
		}
	}
	return false
}

func (m *contentModel) processPending() {
	for len(m.pendingItems) > 0 {
		item := m.pendingItems[0]
		m.pendingItems = m.pendingItems[1:]
		m.items = append(m.items, item)
		if item.Type == "qa" {
			break // Stop at the first QA
		}
	}
}

func (m *contentModel) answerQA(index int) (string, ContentItem) {
	var target *ContentItem
	for i := len(m.items) - 1; i >= 0; i-- {
		if m.items[i].Type == "qa" {
			target = &m.items[i]
			break
		}
	}

	if target != nil && index >= 1 && index <= len(target.Options) {
		selected := target.Options[index-1]
		target.Text = fmt.Sprintf(
			"%s\n%s > %s",
			target.Text,
			m.theme.DimStyle.Render("Answered:"),
			selected,
		)
		target.Type = "notification" // Convert back to standard item
		target.Options = nil
		if target.ResponseChan != nil {
			select {
			case target.ResponseChan <- selected:
			default:
				// Channel might be closed or nobody's listening
			}
		}
		m.processPending()
		return selected, *target
	}
	return "", ContentItem{}
}

func (m *contentModel) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	contentWidth := m.width - 3 // 2 for scrollbar and margin, 1 for border

	var renderedItems []string
	for i, item := range m.items {
		renderedItems = append(
			renderedItems,
			m.renderItem(item, contentWidth, i == m.selectedIndex),
		)
	}

	contentStr := strings.Join(renderedItems, "\n\n")
	lines := strings.Split(contentStr, "\n")

	maxScroll := len(lines) - m.height
	if maxScroll < 0 {
		maxScroll = 0
	}

	// If we were not sticky but we reached the bottom (via manual scroll), re-enable it
	if !m.stickyBottom && m.scrollOffset >= maxScroll {
		m.stickyBottom = true
	}

	if m.stickyBottom {
		m.scrollOffset = maxScroll
	} else if m.scrollOffset > maxScroll {
		m.scrollOffset = maxScroll
	}

	end := m.scrollOffset + m.height
	if end > len(lines) {
		end = len(lines)
	}

	visibleLines := lines[m.scrollOffset:end]
	scrollbar := m.renderScrollbar(len(lines), m.height, m.scrollOffset)

	contentArea := strings.Join(visibleLines, "\n")
	for len(visibleLines) < m.height {
		contentArea += "\n"
		visibleLines = append(visibleLines, "")
	}

	contentBox := lipgloss.NewStyle().Width(contentWidth).Render(contentArea)
	return lipgloss.JoinHorizontal(lipgloss.Top, contentBox, " ", scrollbar)
}

func (m *contentModel) renderItem(item ContentItem, width int, selected bool) string {
	style := m.theme.InactiveItem.Copy().Width(width - 2) // -2 for padding and border
	if item.Type == "qa" || selected {
		style = m.theme.ActiveItem.Copy().Width(width - 2)
	}

	if selected {
		style = style.BorderForeground(m.theme.Primary)
	}

	text := lipgloss.NewStyle().Foreground(m.theme.Text).Render(item.Text)
	if item.Type == "qa" {
		for i, opt := range item.Options {
			optStyle := lipgloss.NewStyle().Foreground(m.theme.Primary)
			text += fmt.Sprintf("\n  [%d] %s", i+1, optStyle.Render(opt))
		}
	}

	return style.Render(text)
}

func (m *contentModel) renderScrollbar(totalLines, viewHeight, offset int) string {
	trackStyle := lipgloss.NewStyle().
		Foreground(m.theme.Dim)

	thumbStyle := lipgloss.NewStyle().
		Foreground(m.theme.Dim) // Or highlight? We'll stick with dim for now as requested.

	if totalLines <= viewHeight || viewHeight == 0 {
		return trackStyle.Render(strings.Repeat("┃\n", viewHeight))
	}

	thumbSize := (viewHeight * viewHeight) / totalLines
	if thumbSize < 1 {
		thumbSize = 1
	}

	maxOffset := totalLines - viewHeight
	thumbPos := 0
	if maxOffset > 0 {
		thumbPos = (offset * (viewHeight - thumbSize)) / maxOffset
	}

	var sb strings.Builder
	for i := 0; i < viewHeight; i++ {
		char := "░"
		style := trackStyle
		if i >= thumbPos && i < thumbPos+thumbSize {
			char = "█"
			style = thumbStyle
		}
		sb.WriteString(style.Render(char))
		if i < viewHeight-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}
