package zk_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/brightpuddle/clara/internal/mcpserver/zk"
)

// ── Vault construction helpers ────────────────────────────────────────────────

func writeNote(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name+".md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestVault_OpenAndList(t *testing.T) {
	dir := t.TempDir()
	writeNote(t, dir, "alpha", "# Alpha\nHello world")
	writeNote(t, dir, "beta", "# Beta\nHello world")

	v, err := zk.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	notes := v.AllNotes()
	if len(notes) != 2 {
		t.Fatalf("expected 2 notes, got %d", len(notes))
	}
}

func TestVault_NoteByName(t *testing.T) {
	dir := t.TempDir()
	writeNote(t, dir, "My Note", "# My Note")

	v, err := zk.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	cases := []struct {
		query string
		found bool
	}{
		{"My Note", true},
		{"my note", true}, // case-insensitive
		{"MY NOTE", true},
		{"other", false},
	}
	for _, tc := range cases {
		n, ok := v.NoteByName(tc.query)
		if ok != tc.found {
			t.Errorf("NoteByName(%q): got found=%v, want %v", tc.query, ok, tc.found)
		}
		if ok && n.Name != "My Note" {
			t.Errorf("NoteByName(%q): got name=%q, want %q", tc.query, n.Name, "My Note")
		}
	}
}

func TestVault_FrontmatterParsing(t *testing.T) {
	dir := t.TempDir()
	writeNote(t, dir, "note", `---
title: My Title
tags:
  - project
  - work
custom_field: hello
---

Body text.
`)

	v, err := zk.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	n, ok := v.NoteByName("note")
	if !ok {
		t.Fatal("expected note to be indexed")
	}

	if n.Frontmatter == nil {
		t.Fatal("expected non-nil frontmatter")
	}
	if title, ok := n.Frontmatter["title"].(string); !ok || title != "My Title" {
		t.Errorf("frontmatter title: got %v", n.Frontmatter["title"])
	}
	if cf, ok := n.Frontmatter["custom_field"].(string); !ok || cf != "hello" {
		t.Errorf("frontmatter custom_field: got %v", n.Frontmatter["custom_field"])
	}
}

func TestVault_FrontmatterTags(t *testing.T) {
	dir := t.TempDir()
	writeNote(t, dir, "note", `---
tags: [project, work]
---

Body.
`)

	v, err := zk.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	n, ok := v.NoteByName("note")
	if !ok {
		t.Fatal("note not found")
	}

	wantTags := []string{"project", "work"}
	sort.Strings(n.Tags)
	sort.Strings(wantTags)
	if len(n.Tags) != len(wantTags) {
		t.Fatalf("tags: got %v, want %v", n.Tags, wantTags)
	}
	for i := range wantTags {
		if n.Tags[i] != wantTags[i] {
			t.Errorf("tag[%d]: got %q, want %q", i, n.Tags[i], wantTags[i])
		}
	}
}

func TestVault_InlineTags(t *testing.T) {
	dir := t.TempDir()
	writeNote(t, dir, "note", `# Note

This is tagged with #project and #work/deep tags.
Also #UPPERCASE should be normalised.
`)

	v, err := zk.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	n, ok := v.NoteByName("note")
	if !ok {
		t.Fatal("note not found")
	}

	tagSet := make(map[string]bool)
	for _, t := range n.Tags {
		tagSet[t] = true
	}
	for _, want := range []string{"project", "work/deep", "uppercase"} {
		if !tagSet[want] {
			t.Errorf("expected tag %q in %v", want, n.Tags)
		}
	}
}

func TestVault_NotesByTag(t *testing.T) {
	dir := t.TempDir()
	writeNote(t, dir, "a", "# A\n#project")
	writeNote(t, dir, "b", "# B\n#project #work")
	writeNote(t, dir, "c", "# C\n#work")

	v, err := zk.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	projectNotes := v.NotesByTag("project")
	if len(projectNotes) != 2 {
		t.Errorf("NotesByTag(project): got %d notes, want 2", len(projectNotes))
	}

	workNotes := v.NotesByTag("#work") // leading # should be stripped
	if len(workNotes) != 2 {
		t.Errorf("NotesByTag(#work): got %d notes, want 2", len(workNotes))
	}

	emptyNotes := v.NotesByTag("nonexistent")
	if len(emptyNotes) != 0 {
		t.Errorf("NotesByTag(nonexistent): expected empty, got %v", emptyNotes)
	}
}

func TestVault_WikilinkExtraction(t *testing.T) {
	dir := t.TempDir()
	writeNote(t, dir, "source", `# Source

See [[Target Note]] and [[Another Note|custom label]] and [[Target Note]] again.
Also [[Note#Section]] which has a fragment.
`)
	writeNote(t, dir, "Target Note", "# Target")
	writeNote(t, dir, "Another Note", "# Another")

	v, err := zk.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	src, ok := v.NoteByName("source")
	if !ok {
		t.Fatal("source note not found")
	}

	wlSet := make(map[string]bool)
	for _, wl := range src.Wikilinks {
		wlSet[wl] = true
	}

	// Duplicates should be deduplicated.
	if !wlSet["Target Note"] {
		t.Errorf("expected wikilink %q, got %v", "Target Note", src.Wikilinks)
	}
	if !wlSet["Another Note"] {
		t.Errorf("expected wikilink %q, got %v", "Another Note", src.Wikilinks)
	}
	// Fragment stripped → "Note"
	if !wlSet["Note"] {
		t.Errorf("expected fragment-stripped wikilink %q, got %v", "Note", src.Wikilinks)
	}
	// No duplicates.
	count := 0
	for _, wl := range src.Wikilinks {
		if wl == "Target Note" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 occurrence of Target Note, got %d", count)
	}
}

func TestVault_ResolveWikilink(t *testing.T) {
	dir := t.TempDir()
	targetPath := writeNote(t, dir, "My Target", "# Target")

	v, err := zk.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	cases := []struct {
		target string
		want   string
	}{
		{"My Target", targetPath},
		{"my target", targetPath}, // case-insensitive
		{"My Target#Section", targetPath},
		{"nonexistent", ""},
	}
	for _, tc := range cases {
		got := v.ResolveWikilink(tc.target)
		if got != tc.want {
			t.Errorf("ResolveWikilink(%q): got %q, want %q", tc.target, got, tc.want)
		}
	}
}

func TestVault_CRUD(t *testing.T) {
	dir := t.TempDir()
	v, err := zk.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Create via IndexPath (server.go does write then IndexPath)
	content := "---\ntags: [test]\n---\n# New Note\n"
	p := filepath.Join(dir, "new-note.md")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := v.IndexPath(p); err != nil {
		t.Fatalf("IndexPath: %v", err)
	}

	n, ok := v.NoteByName("new-note")
	if !ok {
		t.Fatal("new-note not found after IndexPath")
	}
	if len(n.Tags) == 0 || n.Tags[0] != "test" {
		t.Errorf("expected tag 'test', got %v", n.Tags)
	}

	// Update
	updated := "---\ntags: [updated]\n---\n# Updated\n"
	if err := os.WriteFile(p, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := v.IndexPath(p); err != nil {
		t.Fatalf("IndexPath after update: %v", err)
	}
	n2, _ := v.NoteByName("new-note")
	if len(n2.Tags) == 0 || n2.Tags[0] != "updated" {
		t.Errorf("expected tag 'updated' after re-index, got %v", n2.Tags)
	}
	// Old tag should no longer be indexed.
	testNotes := v.NotesByTag("test")
	if len(testNotes) != 0 {
		t.Errorf("expected old tag removed, still found %d notes", len(testNotes))
	}

	// Delete
	v.RemovePath(p)
	if _, ok := v.NoteByName("new-note"); ok {
		t.Error("note still present after RemovePath")
	}
}

func TestVault_SubdirectoryAndSymlink(t *testing.T) {
	dir := t.TempDir()
	// Note in a subdirectory.
	writeNote(t, dir, filepath.Join("subdir", "nested"), "# Nested")

	// Symlink pointing to a note outside the vault root.
	external := t.TempDir()
	extNote := filepath.Join(external, "external.md")
	if err := os.WriteFile(extNote, []byte("# External"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(dir, "linked.md")
	if err := os.Symlink(extNote, linkPath); err != nil {
		t.Skip("symlinks not supported on this OS:", err)
	}

	v, err := zk.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, ok := v.NoteByName("nested"); !ok {
		t.Error("expected nested note to be indexed")
	}
	if _, ok := v.NoteByName("external"); !ok {
		t.Error("expected symlinked note to be indexed")
	}
}

func TestVault_InlineTagsDoNotMatchHexColors(t *testing.T) {
	dir := t.TempDir()
	writeNote(t, dir, "note", `# Note

Color: (#ff0000) is red. This should not produce a tag.
But #realTag should be a tag.
`)

	v, err := zk.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	n, _ := v.NoteByName("note")
	tagSet := make(map[string]bool)
	for _, tag := range n.Tags {
		tagSet[tag] = true
	}

	if tagSet["ff0000"] {
		t.Error("hex color #ff0000 should not be parsed as a tag")
	}
	if !tagSet["realtag"] {
		t.Errorf("expected #realTag to be indexed, got tags: %v", n.Tags)
	}
}

func TestVault_NoNonMarkdownFiles(t *testing.T) {
	dir := t.TempDir()
	writeNote(t, dir, "note", "# Note")
	// Write a non-Markdown file.
	if err := os.WriteFile(filepath.Join(dir, "image.png"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "doc.txt"), []byte("text"), 0o644); err != nil {
		t.Fatal(err)
	}

	v, err := zk.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	notes := v.AllNotes()
	if len(notes) != 1 {
		t.Errorf("expected 1 note (only .md), got %d: %v", len(notes), func() []string {
			var names []string
			for _, n := range notes {
				names = append(names, n.Name)
			}
			return names
		}())
	}
}
