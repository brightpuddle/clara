package panes

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	artifactv1 "github.com/brightpuddle/clara/gen/artifact/v1"
)

func makeArtifact(id, title string) *artifactv1.Artifact {
	return &artifactv1.Artifact{
		Id:    id,
		Title: title,
		Kind:  artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE,
	}
}

func TestArtifactsPane_SetArtifacts_Basic(t *testing.T) {
	p := NewArtifactsPane()
	arts := []*artifactv1.Artifact{
		makeArtifact("a1", "Alpha"),
		makeArtifact("a2", "Beta"),
	}
	p.SetArtifacts(arts)

	if sel := p.Selected(); sel == nil {
		t.Fatal("expected selected artifact, got nil")
	} else if sel.Id != "a1" {
		t.Errorf("expected first artifact selected, got %q", sel.Id)
	}
}

func TestArtifactsPane_SetArtifacts_PreservesSelection(t *testing.T) {
	p := NewArtifactsPane()
	p.SetSize(40, 20)

	arts := []*artifactv1.Artifact{
		makeArtifact("a1", "Alpha"),
		makeArtifact("a2", "Beta"),
		makeArtifact("a3", "Gamma"),
	}
	p.SetArtifacts(arts)

	// Navigate to the second item.
	p.Update(makeKeyMsg("j"))
	if sel := p.Selected(); sel == nil || sel.Id != "a2" {
		t.Fatalf("expected a2 selected after j, got %v", sel)
	}

	// Reload same artifacts — cursor should remain on a2.
	p.SetArtifacts(arts)
	if sel := p.Selected(); sel == nil || sel.Id != "a2" {
		t.Errorf("expected a2 preserved after reload, got %v", sel)
	}
}

func TestArtifactsPane_SetArtifacts_FallsToFirstIfGone(t *testing.T) {
	p := NewArtifactsPane()
	p.SetSize(40, 20)

	arts := []*artifactv1.Artifact{
		makeArtifact("a1", "Alpha"),
		makeArtifact("a2", "Beta"),
	}
	p.SetArtifacts(arts)
	p.Update(makeKeyMsg("j")) // select a2

	// Reload without a2 — should fall to first (a1).
	p.SetArtifacts([]*artifactv1.Artifact{makeArtifact("a1", "Alpha")})
	if sel := p.Selected(); sel == nil || sel.Id != "a1" {
		t.Errorf("expected a1 after removed selection, got %v", sel)
	}
}

func TestArtifactsPane_Navigation(t *testing.T) {
	p := NewArtifactsPane()
	p.SetSize(40, 20)
	arts := []*artifactv1.Artifact{
		makeArtifact("a1", "Alpha"),
		makeArtifact("a2", "Beta"),
		makeArtifact("a3", "Gamma"),
	}
	p.SetArtifacts(arts)

	// j moves down
	p.Update(makeKeyMsg("j"))
	if sel := p.Selected(); sel == nil || sel.Id != "a2" {
		t.Errorf("after j: expected a2, got %v", sel)
	}

	// k moves up
	p.Update(makeKeyMsg("k"))
	if sel := p.Selected(); sel == nil || sel.Id != "a1" {
		t.Errorf("after k: expected a1, got %v", sel)
	}

	// k at top stays
	p.Update(makeKeyMsg("k"))
	if sel := p.Selected(); sel == nil || sel.Id != "a1" {
		t.Errorf("after k at top: expected a1, got %v", sel)
	}
}

func TestArtifactsPane_GGJumpToTop(t *testing.T) {
	p := NewArtifactsPane()
	p.SetSize(40, 20)
	arts := []*artifactv1.Artifact{
		makeArtifact("a1", "Alpha"),
		makeArtifact("a2", "Beta"),
		makeArtifact("a3", "Gamma"),
	}
	p.SetArtifacts(arts)
	p.Update(makeKeyMsg("G")) // go to bottom
	p.Update(makeKeyMsg("g"))
	p.Update(makeKeyMsg("g")) // gg = go to top
	if sel := p.Selected(); sel == nil || sel.Id != "a1" {
		t.Errorf("after gg: expected a1, got %v", sel)
	}
}

func TestArtifactsPane_GJumpToBottom(t *testing.T) {
	p := NewArtifactsPane()
	p.SetSize(40, 20)
	arts := []*artifactv1.Artifact{
		makeArtifact("a1", "Alpha"),
		makeArtifact("a2", "Beta"),
		makeArtifact("a3", "Gamma"),
	}
	p.SetArtifacts(arts)
	p.Update(makeKeyMsg("G"))
	if sel := p.Selected(); sel == nil || sel.Id != "a3" {
		t.Errorf("after G: expected a3, got %v", sel)
	}
}

func TestArtifactsPane_Search(t *testing.T) {
	p := NewArtifactsPane()
	p.SetSize(40, 20)
	p.SetFocused(true)
	arts := []*artifactv1.Artifact{
		makeArtifact("a1", "Alpha planning"),
		makeArtifact("a2", "Beta notes"),
		makeArtifact("a3", "Gamma planning"),
	}
	p.SetArtifacts(arts)

	// Start search with /
	p.Update(makeKeyMsg("/"))
	if !p.IsSearching() {
		t.Fatal("expected searching after /")
	}

	// Type "planning"
	for _, ch := range "planning" {
		p.Update(makeKeyMsgRune(ch))
	}

	// Should have 2 filtered results
	if len(p.filtered) != 2 {
		t.Errorf("expected 2 search results, got %d", len(p.filtered))
	}

	// Escape clears search
	p.Update(makeKeyMsg("esc"))
	if p.IsSearching() {
		t.Error("expected not searching after esc")
	}
	if len(p.filtered) != 3 {
		t.Errorf("expected 3 results after esc, got %d", len(p.filtered))
	}
}

func TestArtifactsPane_Actions(t *testing.T) {
	p := NewArtifactsPane()
	p.SetSize(40, 20)
	p.SetFocused(true)
	p.SetArtifacts([]*artifactv1.Artifact{makeArtifact("a1", "Alpha")})

	// Space → done action
	action := p.Update(makeKeyMsg(" "))
	if action != "done:a1" {
		t.Errorf("space: got %q, want %q", action, "done:a1")
	}

	// Enter → edit action
	action = p.Update(makeKeyMsg("enter"))
	if action != "edit:a1" {
		t.Errorf("enter: got %q, want %q", action, "edit:a1")
	}

	// o → open action
	action = p.Update(makeKeyMsg("o"))
	if action != "open:a1" {
		t.Errorf("o: got %q, want %q", action, "open:a1")
	}
}

func TestArtifactsPane_Empty(t *testing.T) {
	p := NewArtifactsPane()
	if sel := p.Selected(); sel != nil {
		t.Errorf("expected nil selection on empty pane, got %v", sel)
	}
}

// makeKeyMsg creates a tea.KeyMsg for a key string.
// For rune keys (j, k, /, etc.) uses KeyRunes; String() returns the key string.
func makeKeyMsg(key string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}

// makeKeyMsgRune creates a tea.KeyMsg for a single rune.
func makeKeyMsgRune(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}
