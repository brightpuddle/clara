package tui

import (
	"strings"
	"testing"
)

func TestPromptHeightClipping(t *testing.T) {
	theme := DefaultTheme()
	pm := newPromptModel(theme)
	pm.SetSize(50, 5)

	// Set some multiline text
	pm.input.SetValue("Line 1\nLine 2")
	
	view := pm.View()
	t.Logf("Prompt View (multiline):\n%s", view)
	lines := strings.Split(view, "\n")
	
	// We want to see both lines of text plus the border
	if len(lines) < 3 {
		t.Errorf("expected at least 3 lines (border + 2 text lines), got %d lines: %q", len(lines), view)
	}
}

func TestSingleLinePrompt(t *testing.T) {
	theme := DefaultTheme()
	pm := newPromptModel(theme)
	pm.SetSize(50, 2) // Border (1) + Textarea (1)

	view := pm.View()
	t.Logf("Prompt View (single line):\n%s", view)
	
	// Count occurrences of the prompt character
	count := strings.Count(view, "❯")
	if count != 1 {
		t.Errorf("expected 1 prompt character, got %d. View:\n%s", count, view)
	}
}
