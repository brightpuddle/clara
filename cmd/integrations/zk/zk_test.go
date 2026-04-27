package main

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/rs/zerolog"
)

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

func waitReady(t *testing.T, v *Vault) {
	t.Helper()
	if err := v.WaitReady(context.Background()); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
}

func TestVault_OpenAndList(t *testing.T) {
	dir := t.TempDir()
	writeNote(t, dir, "alpha", "# Alpha\nHello world")
	writeNote(t, dir, "beta", "# Beta\nHello world")

	v, err := Open(dir, "", zerolog.Nop())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	waitReady(t, v)

	notes := v.AllNotes()
	if len(notes) != 2 {
		t.Fatalf("expected 2 notes, got %d", len(notes))
	}
}

func TestVault_NoteByName(t *testing.T) {
	dir := t.TempDir()
	writeNote(t, dir, "My Note", "# My Note")

	v, err := Open(dir, "", zerolog.Nop())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	waitReady(t, v)

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

func TestVault_InlineTags(t *testing.T) {
	dir := t.TempDir()
	writeNote(t, dir, "note", "# Note\n\nThis is tagged with #project and #work/deep tags.\n")

	v, err := Open(dir, "", zerolog.Nop())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	waitReady(t, v)

	n, ok := v.NoteByName("note")
	if !ok {
		t.Fatal("note not found")
	}

	tagSet := make(map[string]bool)
	for _, t := range n.Tags {
		tagSet[t] = true
	}
	for _, want := range []string{"project", "work/deep"} {
		if !tagSet[want] {
			t.Errorf("expected tag %q in %v", want, n.Tags)
		}
	}
}

func TestVault_Search(t *testing.T) {
	dir := t.TempDir()
	writeNote(t, dir, "alpha", "# Alpha\nThis contains the word secret.")
	writeNote(t, dir, "beta", "# Beta\nNothing interesting here.")
	writeNote(t, dir, "gamma", "# Gamma\nAnother secret message.")

	v, err := Open(dir, "", zerolog.Nop())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	waitReady(t, v)

	ctx := context.Background()
	results := v.Search(ctx, "secret", 0)
	if len(results) != 2 {
		t.Fatalf("expected 2 search results, got %d", len(results))
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})

	if results[0].Name != "alpha" || results[1].Name != "gamma" {
		t.Errorf("unexpected results: %v, %v", results[0].Name, results[1].Name)
	}
}
