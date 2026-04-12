package tui

import (
	"testing"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func TestStickyBottom_InitialScroll(t *testing.T) {
	theme := DefaultTheme()
	m := newContentModel(theme)
	m.SetSize(100, 5) // Height 5

	// Add enough items to exceed viewport
	for i := 0; i < 10; i++ {
		m.addItem(ContentItem{Type: "notification", Text: "Line " + strings.Repeat("x", 10)})
	}

	// First View() should set scrollOffset to bottom if stickyBottom is true
	m.View()

	if m.scrollOffset == 0 {
		t.Errorf("expected scrollOffset to be at bottom, got 0")
	}
}

func TestStickyBottom_ScrollUpDisables(t *testing.T) {
	theme := DefaultTheme()
	m := newContentModel(theme)
	m.SetSize(100, 5)
	for i := 0; i < 10; i++ {
		m.addItem(ContentItem{Type: "notification", Text: "Line"})
	}
	m.View() // Initialize scrollOffset to bottom

	// Simulate scroll up
	m.Update(tea.MouseMsg{Type: tea.MouseWheelUp})

	if m.stickyBottom {
		t.Errorf("expected stickyBottom to be false after scrolling up")
	}
}

func TestStickyBottom_ScrollDownReenables(t *testing.T) {
	theme := DefaultTheme()
	m := newContentModel(theme)
	m.SetSize(100, 10)
	for i := 0; i < 100; i++ {
		m.addItem(ContentItem{Type: "notification", Text: "Line"})
	}
	m.View() // Force scroll to bottom (stickyBottom=true)
	m.Update(tea.MouseMsg{Type: tea.MouseWheelUp}) // stickyBottom=false

	// Manual scroll down to bottom
	// We need to know maxScroll. View() calculates it but it's internal.
	// We'll just scroll a lot.
	for i := 0; i < 500; i++ {
		m.Update(tea.MouseMsg{Type: tea.MouseWheelDown})
	}

	m.View() // View() should re-enable stickyBottom

	if !m.stickyBottom {
		t.Errorf("expected stickyBottom to be true after scrolling to bottom")
	}
}

func TestStickyBottom_AutoScrollOnNewItem(t *testing.T) {
	theme := DefaultTheme()
	m := newContentModel(theme)
	m.SetSize(100, 5)
	for i := 0; i < 10; i++ {
		m.addItem(ContentItem{Type: "notification", Text: "Line"})
	}
	m.View()
	initialScroll := m.scrollOffset

	// Add another item
	m.addItem(ContentItem{Type: "notification", Text: "New Line"})
	m.View()

	if m.scrollOffset <= initialScroll {
		t.Errorf("expected scrollOffset to increase after adding item, got %d -> %d", initialScroll, m.scrollOffset)
	}
}

func TestStickyBottom_NoAutoScrollWhenNotSticky(t *testing.T) {
	theme := DefaultTheme()
	m := newContentModel(theme)
	m.SetSize(100, 5)
	for i := 0; i < 10; i++ {
		m.addItem(ContentItem{Type: "notification", Text: "Line"})
	}
	m.View()
	m.Update(tea.MouseMsg{Type: tea.MouseWheelUp}) // Disable sticky
	m.View()
	initialScroll := m.scrollOffset

	// Add another item
	m.addItem(ContentItem{Type: "notification", Text: "New Line"})
	m.View()

	if m.scrollOffset != initialScroll {
		t.Errorf("expected scrollOffset to remain stable when not sticky, got %d -> %d", initialScroll, m.scrollOffset)
	}
}
