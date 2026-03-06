package tui

import (
	"os"
	"path/filepath"
	"strings"
)

// notesMaxDepth is the maximum directory recursion depth for walkMarkdownFiles.
// It is a var (not const) so tests can temporarily override it.
var notesMaxDepth = 20

// walkMarkdownFiles returns all markdown (.md / .markdown) files under root,
// following symlinks with two safety mechanisms:
//   - cycle detection via a visited set of real (EvalSymlinks-resolved) paths
//   - notesMaxDepth as a hard cap on recursion depth
func walkMarkdownFiles(root string) ([]string, error) {
	visited := make(map[string]bool)
	return walkDir(root, visited, 0)
}

// walkDir is the recursive implementation used by walkMarkdownFiles.
// dir may be a real path or a symlink; visited tracks resolved paths.
func walkDir(dir string, visited map[string]bool, depth int) ([]string, error) {
	if depth > notesMaxDepth {
		return nil, nil
	}

	// Resolve dir to its real path for cycle detection.
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, nil // unresolvable (broken symlink or permission error) → skip
	}
	if visited[realDir] {
		return nil, nil // already visited via a different path → cycle
	}
	visited[realDir] = true

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil // unreadable directory → skip
	}

	var files []string
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(dir, name)

		switch {
		case entry.Type()&os.ModeSymlink != 0:
			// Dereference the symlink to learn whether it points to a file or dir.
			info, err := os.Stat(path) // Stat follows symlinks
			if err != nil {
				continue
			}
			if info.IsDir() {
				// Recurse into the symlinked directory.
				// The next call to walkDir will resolve the target and detect cycles.
				sub, _ := walkDir(path, visited, depth+1)
				files = append(files, sub...)
			} else if isMarkdownFile(name) {
				files = append(files, path)
			}

		case entry.IsDir():
			// Skip well-known VCS / editor metadata directories.
			if name == ".git" || name == ".obsidian" {
				continue
			}
			sub, _ := walkDir(path, visited, depth+1)
			files = append(files, sub...)

		default:
			if isMarkdownFile(name) {
				files = append(files, path)
			}
		}
	}
	return files, nil
}

// isMarkdownFile returns true for .md and .markdown files (case-insensitive).
func isMarkdownFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".md" || ext == ".markdown"
}
