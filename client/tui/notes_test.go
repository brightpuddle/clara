package tui

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// writeFile creates a file with content, creating parent dirs as needed.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// sortedFiles calls walkMarkdownFiles and returns the sorted basenames.
func sortedFiles(t *testing.T, root string) []string {
	t.Helper()
	files, err := walkMarkdownFiles(root)
	if err != nil {
		t.Fatalf("walkMarkdownFiles(%s): %v", root, err)
	}
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = filepath.Base(f)
	}
	sort.Strings(names)
	return names
}

// TestWalkMarkdownFiles_Basic verifies that only markdown files are returned.
func TestWalkMarkdownFiles_Basic(t *testing.T) {
	tmp := t.TempDir()

	writeFile(t, filepath.Join(tmp, "a.md"), "# A")
	writeFile(t, filepath.Join(tmp, "b.markdown"), "# B")
	writeFile(t, filepath.Join(tmp, "c.txt"), "not a note")
	writeFile(t, filepath.Join(tmp, "d.go"), "package main")

	got := sortedFiles(t, tmp)
	want := []string{"a.md", "b.markdown"}
	if !equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestWalkMarkdownFiles_Recursive verifies subdirectory traversal.
func TestWalkMarkdownFiles_Recursive(t *testing.T) {
	tmp := t.TempDir()

	writeFile(t, filepath.Join(tmp, "root.md"), "# Root")
	writeFile(t, filepath.Join(tmp, "sub", "child.md"), "# Child")
	writeFile(t, filepath.Join(tmp, "sub", "deep", "leaf.md"), "# Leaf")

	got := sortedFiles(t, tmp)
	want := []string{"child.md", "leaf.md", "root.md"}
	if !equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestWalkMarkdownFiles_SkipsGitAndObsidian verifies metadata dirs are ignored.
func TestWalkMarkdownFiles_SkipsGitAndObsidian(t *testing.T) {
	tmp := t.TempDir()

	writeFile(t, filepath.Join(tmp, "note.md"), "# Note")
	writeFile(t, filepath.Join(tmp, ".git", "COMMIT_EDITMSG"), "git file")
	writeFile(t, filepath.Join(tmp, ".obsidian", "config.md"), "obsidian config")

	got := sortedFiles(t, tmp)
	want := []string{"note.md"}
	if !equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestWalkMarkdownFiles_SymlinkFile verifies that symlinked markdown files are included.
func TestWalkMarkdownFiles_SymlinkFile(t *testing.T) {
	tmp := t.TempDir()

	// Real file outside notes dir.
	realFile := filepath.Join(tmp, "_real.md")
	writeFile(t, realFile, "# Real")

	// Symlink inside notes dir pointing at the real file.
	notesDir := filepath.Join(tmp, "notes")
	if err := os.Mkdir(notesDir, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(notesDir, "linked.md")
	if err := os.Symlink(realFile, link); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
	writeFile(t, filepath.Join(notesDir, "direct.md"), "# Direct")

	got := sortedFiles(t, notesDir)
	want := []string{"direct.md", "linked.md"}
	if !equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestWalkMarkdownFiles_SymlinkDir verifies that symlinked directories are followed.
func TestWalkMarkdownFiles_SymlinkDir(t *testing.T) {
	tmp := t.TempDir()

	// Real directory with notes, outside the main notes dir.
	realDir := filepath.Join(tmp, "archive")
	writeFile(t, filepath.Join(realDir, "old.md"), "# Old")

	// Notes dir with a symlink to the archive.
	notesDir := filepath.Join(tmp, "notes")
	if err := os.Mkdir(notesDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(notesDir, "current.md"), "# Current")

	link := filepath.Join(notesDir, "archive")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	got := sortedFiles(t, notesDir)
	want := []string{"current.md", "old.md"}
	if !equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestWalkMarkdownFiles_CycleDetection verifies that symlink cycles do not
// cause infinite recursion and that reachable files are still returned.
func TestWalkMarkdownFiles_CycleDetection(t *testing.T) {
	tmp := t.TempDir()

	notesDir := filepath.Join(tmp, "notes")
	subDir := filepath.Join(notesDir, "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(notesDir, "top.md"), "# Top")
	writeFile(t, filepath.Join(subDir, "sub.md"), "# Sub")

	// Create a cycle: notes/sub/loop → notes (points back to the root).
	loop := filepath.Join(subDir, "loop")
	if err := os.Symlink(notesDir, loop); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	// Walk must terminate and return only the two real files.
	got := sortedFiles(t, notesDir)
	want := []string{"sub.md", "top.md"}
	if !equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestWalkMarkdownFiles_MaxDepth verifies that recursion stops at notesMaxDepth.
func TestWalkMarkdownFiles_MaxDepth(t *testing.T) {
	// Temporarily lower the limit so the test doesn't need dozens of directories.
	orig := notesMaxDepth
	notesMaxDepth = 3
	defer func() { notesMaxDepth = orig }()

	tmp := t.TempDir()

	// File within the depth limit (depth 2).
	writeFile(t, filepath.Join(tmp, "a", "b", "shallow.md"), "# Shallow")

	// File beyond the depth limit (depth 4 = notesMaxDepth+1).
	writeFile(t, filepath.Join(tmp, "a", "b", "c", "d", "deep.md"), "# Deep")

	got := sortedFiles(t, tmp)
	want := []string{"shallow.md"}
	if !equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestWalkMarkdownFiles_BrokenSymlink verifies that broken symlinks are silently skipped.
func TestWalkMarkdownFiles_BrokenSymlink(t *testing.T) {
	tmp := t.TempDir()

	writeFile(t, filepath.Join(tmp, "real.md"), "# Real")

	// Broken symlink (target does not exist).
	broken := filepath.Join(tmp, "broken.md")
	if err := os.Symlink(filepath.Join(tmp, "nonexistent.md"), broken); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	got := sortedFiles(t, tmp)
	want := []string{"real.md"}
	if !equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// equal returns true if a and b contain the same elements in the same order.
func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
