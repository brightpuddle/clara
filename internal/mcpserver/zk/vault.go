// Package zk provides a built-in MCP server for Zettelkasten-style Markdown
// vaults (Obsidian, Zettlr, plain zk, etc.).
//
// On Open, the vault walks the root directory (following symlinks up to
// maxSymlinkDepth levels) and builds an in-memory index of every .md file.
// The index maps:
//   - stem (lower-cased filename without extension) → Note
//   - tag → []Note
//
// All mutations (create/update/delete) invalidate the affected note entry and
// refresh the index incrementally so the server stays fast for large vaults.
package zk

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"go.abhg.dev/goldmark/frontmatter"
	"go.abhg.dev/goldmark/wikilink"
)

const maxSymlinkDepth = 5

// Note holds the indexed metadata for a single Markdown file.
type Note struct {
	// Path is the absolute path to the .md file (symlinks resolved one level).
	Path string
	// Name is the filename stem (no extension, case-preserved).
	Name string
	// Frontmatter holds every key/value pair from the YAML front-matter block.
	Frontmatter map[string]any
	// Tags contains the union of front-matter tags and inline #tag references.
	Tags []string
	// Wikilinks lists every [[target]] referenced in the note body.
	Wikilinks []string
}

// Vault is a thread-safe, indexed view of a Zettelkasten vault directory.
type Vault struct {
	root string

	mu          sync.RWMutex
	notesByPath map[string]*Note // absolute path → Note
	notesByName map[string]*Note // lower-cased stem → Note
	notesByTag  map[string][]*Note
}

// Open walks root and builds the initial in-memory index.
func Open(root string) (*Vault, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, errors.Wrap(err, "resolve vault root")
	}
	v := &Vault{
		root:        abs,
		notesByPath: make(map[string]*Note),
		notesByName: make(map[string]*Note),
		notesByTag:  make(map[string][]*Note),
	}
	if err := v.rebuild(); err != nil {
		return nil, err
	}
	return v, nil
}

// Root returns the absolute path of the vault root.
func (v *Vault) Root() string { return v.root }

// ── Index queries ─────────────────────────────────────────────────────────────

// NoteByName looks up a note by its filename stem (case-insensitive).
func (v *Vault) NoteByName(name string) (*Note, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	n, ok := v.notesByName[strings.ToLower(name)]
	return n, ok
}

// NoteByPath looks up a note by its absolute path.
func (v *Vault) NoteByPath(path string) (*Note, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	n, ok := v.notesByPath[path]
	return n, ok
}

// NotesByTag returns all notes that carry the given tag.
func (v *Vault) NotesByTag(tag string) []*Note {
	v.mu.RLock()
	defer v.mu.RUnlock()
	tag = strings.ToLower(strings.TrimPrefix(tag, "#"))
	return append([]*Note(nil), v.notesByTag[tag]...)
}

// AllNotes returns a snapshot of all indexed notes sorted by path.
func (v *Vault) AllNotes() []*Note {
	v.mu.RLock()
	defer v.mu.RUnlock()
	notes := make([]*Note, 0, len(v.notesByPath))
	for _, n := range v.notesByPath {
		notes = append(notes, n)
	}
	return notes
}

// ResolveWikilink returns the absolute path of the note that target points to,
// or "" if no such note exists in the vault.
func (v *Vault) ResolveWikilink(target string) string {
	// Strip any fragment (e.g. "Note#Section").
	if idx := strings.IndexByte(target, '#'); idx >= 0 {
		target = target[:idx]
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	n, ok := v.NoteByName(target)
	if !ok {
		return ""
	}
	return n.Path
}

// ── Mutations ─────────────────────────────────────────────────────────────────

// IndexPath (re-)indexes a single file, replacing any existing entry.
// Call this after creating or updating a note.
func (v *Vault) IndexPath(path string) error {
	note, err := parseNote(path)
	if err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.removeNoteLocked(path)
	v.insertNoteLocked(note)
	return nil
}

// RemovePath removes a note from the index without touching the file.
func (v *Vault) RemovePath(path string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.removeNoteLocked(path)
}

// ── Internal build / walk ─────────────────────────────────────────────────────

func (v *Vault) rebuild() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.notesByPath = make(map[string]*Note)
	v.notesByName = make(map[string]*Note)
	v.notesByTag = make(map[string][]*Note)

	return walkVault(v.root, 0, func(path string) error {
		note, err := parseNote(path)
		if err != nil {
			// Silently skip unreadable/unparseable notes so one bad file
			// cannot block the entire vault index.
			return nil
		}
		v.insertNoteLocked(note)
		return nil
	})
}

func (v *Vault) insertNoteLocked(n *Note) {
	v.notesByPath[n.Path] = n
	v.notesByName[strings.ToLower(n.Name)] = n
	for _, tag := range n.Tags {
		v.notesByTag[tag] = append(v.notesByTag[tag], n)
	}
}

func (v *Vault) removeNoteLocked(path string) {
	old, ok := v.notesByPath[path]
	if !ok {
		return
	}
	delete(v.notesByPath, path)
	delete(v.notesByName, strings.ToLower(old.Name))
	for _, tag := range old.Tags {
		list := v.notesByTag[tag]
		out := list[:0]
		for _, n := range list {
			if n.Path != path {
				out = append(out, n)
			}
		}
		if len(out) == 0 {
			delete(v.notesByTag, tag)
		} else {
			v.notesByTag[tag] = out
		}
	}
}

// walkVault recursively visits every .md file under root, following symlinks
// up to maxSymlinkDepth levels deep.
func walkVault(root string, depth int, visit func(string) error) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return errors.Wrapf(err, "read vault dir %q", root)
	}
	for _, e := range entries {
		fullPath := filepath.Join(root, e.Name())

		// Resolve one level of symlink to determine what it points to.
		if e.Type()&os.ModeSymlink != 0 {
			if depth >= maxSymlinkDepth {
				continue // prevent infinite symlink loops
			}
			target, err := filepath.EvalSymlinks(fullPath)
			if err != nil {
				continue
			}
			info, err := os.Stat(target)
			if err != nil {
				continue
			}
			if info.IsDir() {
				if err := walkVault(target, depth+1, visit); err != nil {
					return err
				}
				continue
			}
			if isMarkdown(target) {
				if err := visit(target); err != nil {
					return err
				}
			}
			continue
		}

		if e.IsDir() {
			if err := walkVault(fullPath, depth, visit); err != nil {
				return err
			}
			continue
		}

		if isMarkdown(fullPath) {
			if err := visit(fullPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func isMarkdown(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".md" || ext == ".markdown"
}

// ── Goldmark parser setup ─────────────────────────────────────────────────────

var mdParser = goldmark.New(
	goldmark.WithExtensions(
		&frontmatter.Extender{},
		&wikilink.Extender{},
	),
)

// parseNote reads the file at path and extracts frontmatter, tags, and
// wikilinks into a Note.
func parseNote(path string) (*Note, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "read note %q", path)
	}

	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))

	ctx := parser.NewContext()
	var buf bytes.Buffer
	if err := mdParser.Convert(data, &buf, parser.WithContext(ctx)); err != nil {
		return nil, errors.Wrapf(err, "parse note %q", path)
	}

	// ── Frontmatter ───────────────────────────────────────────────────────
	var fm map[string]any
	if d := frontmatter.Get(ctx); d != nil {
		_ = d.Decode(&fm)
	}

	// ── Tags ──────────────────────────────────────────────────────────────
	tagSet := make(map[string]struct{})

	// Pull tags from frontmatter ("tags" field can be []any or string).
	if fm != nil {
		switch v := fm["tags"].(type) {
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					tagSet[normalizeTag(s)] = struct{}{}
				}
			}
		case string:
			if v != "" {
				tagSet[normalizeTag(v)] = struct{}{}
			}
		}
	}

	// Pull inline #tags by scanning the raw source for #word tokens.
	extractInlineTags(data, tagSet)

	tags := make([]string, 0, len(tagSet))
	for t := range tagSet {
		tags = append(tags, t)
	}

	// ── Wikilinks ─────────────────────────────────────────────────────────
	// Re-parse so we can walk the AST for wikilink nodes.
	doc := mdParser.Parser().Parse(text.NewReader(data))
	wikilinkTargets := extractWikilinks(doc, data)

	return &Note{
		Path:        path,
		Name:        stem,
		Frontmatter: fm,
		Tags:        tags,
		Wikilinks:   wikilinkTargets,
	}, nil
}

// extractInlineTags scans raw Markdown bytes for #tag patterns outside of
// code blocks and frontmatter.
func extractInlineTags(src []byte, out map[string]struct{}) {
	inFence := false
	for _, line := range bytes.Split(src, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		// Skip frontmatter delimiter lines.
		if bytes.Equal(trimmed, []byte("---")) || bytes.Equal(trimmed, []byte("+++")) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		// Skip code fences.
		if bytes.HasPrefix(trimmed, []byte("```")) || bytes.HasPrefix(trimmed, []byte("~~~")) {
			continue
		}
		// Scan for #tag tokens.
		i := 0
		for i < len(line) {
			idx := bytes.IndexByte(line[i:], '#')
			if idx < 0 {
				break
			}
			pos := i + idx
			// Must not be preceded by a non-space/non-start character to
			// avoid matching color hex codes, URL fragments, or Markdown
			// link destinations. A valid inline tag must appear after
			// whitespace (or at the start of a line).
			if pos > 0 {
				prev := line[pos-1]
				if prev != ' ' && prev != '\t' {
					i = pos + 1
					continue
				}
			}
			// Collect tag body: alphanumeric, hyphens, underscores, slashes
			// (for nested tags like #area/topic).
			start := pos + 1
			end := start
			for end < len(line) {
				c := line[end]
				if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
					(c >= '0' && c <= '9') || c == '-' || c == '_' || c == '/' {
					end++
				} else {
					break
				}
			}
			if end > start {
				out[normalizeTag(string(line[start:end]))] = struct{}{}
			}
			i = end
		}
	}
}

// extractWikilinks walks the goldmark AST and collects wikilink target strings.
func extractWikilinks(doc ast.Node, src []byte) []string {
	seen := make(map[string]struct{})
	var targets []string
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		wl, ok := n.(*wikilink.Node)
		if !ok {
			return ast.WalkContinue, nil
		}
		target := strings.TrimSpace(string(wl.Target))
		if target == "" {
			return ast.WalkContinue, nil
		}
		// Strip fragment.
		if idx := strings.IndexByte(target, '#'); idx >= 0 {
			target = target[:idx]
		}
		target = strings.TrimSpace(target)
		if target != "" {
			if _, dup := seen[target]; !dup {
				seen[target] = struct{}{}
				targets = append(targets, target)
			}
		}
		return ast.WalkContinue, nil
	})
	return targets
}

func normalizeTag(s string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(s), "#"))
}
