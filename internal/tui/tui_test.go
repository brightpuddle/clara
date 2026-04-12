package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestModelLayout(t *testing.T) {
	theme := DefaultTheme()
	app := &appModel{
		theme:   theme,
		content: newContentModel(theme),
		prompt:  newPromptModel(theme),
	}

	// Test layout at various sizes
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 50})
	view := app.View()
	if view == "" {
		t.Errorf("expected non-empty view")
	}

	app.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	viewLarge := app.View()
	if viewLarge == "" {
		t.Errorf("expected non-empty view at large width")
	}
}

func TestContentModelQA(t *testing.T) {
	theme := DefaultTheme()
	cm := newContentModel(theme)

	cm.addItem(ContentItem{
		Type:    "qa",
		Text:    "Choose an option",
		Options: []string{"First", "Second", "Third"},
	})

	if !cm.hasActiveQA() {
		t.Errorf("expected active QA to be detected")
	}

	// Invalid answer
	cm.answerQA(5)
	if !cm.hasActiveQA() {
		t.Errorf("expected QA to still be active after invalid answer")
	}

	// Valid answer
	cm.answerQA(2)

	if cm.hasActiveQA() {
		t.Errorf("expected QA to be resolved after valid answer")
	}

	last := cm.items[len(cm.items)-1]
	if last.Type != "notification" {
		t.Errorf("expected item type to be demoted to notification, got %s", last.Type)
	}
}

func TestBuriedQA(t *testing.T) {
	theme := DefaultTheme()
	cm := newContentModel(theme)

	// Add a QA item
	cm.addItem(ContentItem{
		Type:         "qa",
		Text:         "Choose an option",
		Options:      []string{"First", "Second", "Third"},
		ResponseChan: make(chan string, 1),
	})

	// Bury it with notifications
	cm.addItem(ContentItem{
		Type: "notification",
		Text: "System update...",
	})
	cm.addItem(ContentItem{
		Type: "notification",
		Text: "Another notification",
	})

	if !cm.hasActiveQA() {
		t.Errorf("expected active QA to be detected even when buried")
	}

	// Valid answer to the buried QA
	cm.answerQA(2)

	if cm.hasActiveQA() {
		t.Errorf("expected QA to be resolved after valid answer")
	}

	// Verify the correct item was updated
	found := false
	for _, item := range cm.items {
		if strings.Contains(item.Text, "Answered: > Second") {
			found = true
			if item.Type != "notification" {
				t.Errorf("expected answered item type to be notification, got %s", item.Type)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected to find the answered QA item in history")
	}
}

func TestPromptModel(t *testing.T) {
	theme := DefaultTheme()
	pm := newPromptModel(theme)
	pm.SetSize(50, 5)

	view := pm.View()
	if view == "" {
		t.Errorf("expected prompt view not to be empty")
	}

	pm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("test")})
}
