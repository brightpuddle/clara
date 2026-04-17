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
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/brightpuddle/clara/internal/mcpserver/markdown"
	"github.com/brightpuddle/clara/internal/search"
	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"
)

const maxSymlinkDepth = 5

// Note holds the indexed metadata for a single Markdown file.
type Note struct {
	// Path is the absolute path to the .md file (symlinks resolved one level).
	Path string
	// Name is the filename stem (no extension, case-preserved).
	Name string
	// Content is the raw Markdown content of the note.
	Content string
	// Frontmatter holds every key/value pair from the YAML front-matter block.
	Frontmatter map[string]any
	// Tags contains the union of front-matter tags and inline #tag references.
	Tags []string
	// Wikilinks lists every [[target]] referenced in the note body.
	Wikilinks []string
}

// Vault is a thread-safe, indexed view of a Zettelkasten vault directory.
type Vault struct {
	root      string
	indexPath string
	log       zerolog.Logger

	mu          sync.RWMutex
	notesByPath map[string]*Note // absolute path → Note
	notesByName map[string]*Note // lower-cased stem → Note
	notesByTag  map[string][]*Note
	isIndexing  bool

	indexer *search.Indexer
}

// Open walks root and builds the initial in-memory index.
func Open(root string, indexPath string, log zerolog.Logger) (*Vault, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, errors.Wrap(err, "resolve vault root")
	}

	var indexer *search.Indexer
	if indexPath != "" {
		schema := &search.IndexSchema{
			Name:    "zk",
			Columns: []string{"title", "body", "tags"},
		}
		indexer, err = search.NewIndexer(indexPath, schema)
		if err != nil {
			return nil, errors.Wrap(err, "init zk search indexer")
		}
	}

	v := &Vault{
		root:        abs,
		indexPath:   indexPath,
		log:         log,
		notesByPath: make(map[string]*Note),
		notesByName: make(map[string]*Note),
		notesByTag:  make(map[string][]*Note),
		isIndexing:  true,
		indexer:     indexer,
	}
	// Start initial rebuild in the background so Open returns immediately.
	// This ensures the MCP server can respond to handshakes quickly.
	go func() {
		if err := v.rebuild(); err != nil {
			v.log.Error().Err(err).Msg("initial vault indexing failed")
		}
	}()
	return v, nil
}

// IsIndexing reports whether the vault is currently building its index.
func (v *Vault) IsIndexing() bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.isIndexing
}

// WaitReady blocks until the vault index is fully built or ctx is cancelled.
func (v *Vault) WaitReady(ctx context.Context) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		if !v.IsIndexing() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
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

// Search returns all notes whose content matches the given query.
// If an FTS5 index is available, it uses full-text search. Otherwise, it
// performs a simple substring search across all notes (slower).
func (v *Vault) Search(ctx context.Context, query string, limit int) []*Note {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.indexer != nil {
		results, err := v.indexer.Search(ctx, query, limit)
		if err != nil {
			v.log.Error().Err(err).Str("query", query).Msg("fts5 search failed")
		} else {
			var notes []*Note
			for _, res := range results {
				if n, ok := v.notesByPath[res.ID]; ok {
					notes = append(notes, n)
				}
			}
			return notes
		}
	}

	// Fallback to slow substring search
	var results []*Note
	query = strings.ToLower(query)

	for _, n := range v.notesByPath {
		if limit > 0 && len(results) >= limit {
			break
		}
		content, err := os.ReadFile(n.Path)
		if err != nil {
			v.log.Error().Err(err).Str("path", n.Path).Msg("failed to read note for search")
			continue
		}
		if strings.Contains(strings.ToLower(string(content)), query) {
			results = append(results, n)
		}
	}
	return results
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
func (v *Vault) IndexPath(ctx context.Context, path string) error {
	note, err := parseNote(path)
	if err != nil {
		return err
	}

	if v.indexer != nil {
		if err := v.indexNote(ctx, note); err != nil {
			v.log.Error().Err(err).Str("path", path).Msg("failed to update fts5 index")
		}
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	v.removeNoteLocked(path)
	v.insertNoteLocked(note)
	return nil
}

// RemovePath removes a note from the index without touching the file.
func (v *Vault) RemovePath(ctx context.Context, path string) {
	if v.indexer != nil {
		if err := v.indexer.Delete(ctx, path); err != nil {
			v.log.Error().Err(err).Str("path", path).Msg("failed to delete from fts5 index")
		}
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	v.removeNoteLocked(path)
}

func (v *Vault) indexNote(ctx context.Context, n *Note) error {
	if v.indexer == nil {
		return nil
	}
	doc := &search.Document{
		ID: n.Path,
		Data: map[string]string{
			"title": n.Name,
			"body":  n.Content,
			"tags":  strings.Join(n.Tags, " "),
		},
	}
	return v.indexer.Index(ctx, doc)
}

// ── Internal build / walk ─────────────────────────────────────────────────────

func (v *Vault) rebuild() error {
	ctx := context.Background()
	v.mu.Lock()
	v.isIndexing = true
	v.mu.Unlock()
	defer func() {
		v.mu.Lock()
		v.isIndexing = false
		v.mu.Unlock()
	}()

	v.log.Info().Str("root", v.root).Msg("starting vault index rebuild")

	var collected []*Note
	var mu sync.Mutex
	visited := make(map[string]struct{})

	err := v.walkVault(v.root, 0, visited, func(path string) error {
		note, err := parseNote(path)
		if err != nil {
			v.log.Debug().Err(err).Str("path", path).Msg("skipping unparseable note")
			return nil
		}
		mu.Lock()
		collected = append(collected, note)
		mu.Unlock()
		return nil
	})
	if err != nil {
		return err
	}

	// Update on-disk FTS5 index in a single transaction if indexer is available.
	if v.indexer != nil {
		tx, err := v.indexer.BeginTx(ctx)
		if err != nil {
			v.log.Error().Err(err).Msg("failed to begin fts5 index transaction")
		} else {
			for _, n := range collected {
				doc := &search.Document{
					ID: n.Path,
					Data: map[string]string{
						"title": n.Name,
						"body":  n.Content,
						"tags":  strings.Join(n.Tags, " "),
					},
				}
				if err := v.indexer.IndexWithTx(ctx, tx, doc); err != nil {
					v.log.Error().Err(err).Str("path", n.Path).Msg("failed to index note during rebuild")
				}
			}
			if err := tx.Commit(); err != nil {
				v.log.Error().Err(err).Msg("failed to commit fts5 index transaction")
			}
		}
	}

	// Swap in the new maps under a single write lock.
	v.mu.Lock()
	defer v.mu.Unlock()

	v.notesByPath = make(map[string]*Note, len(collected))
	v.notesByName = make(map[string]*Note, len(collected))
	v.notesByTag = make(map[string][]*Note)

	for _, n := range collected {
		v.insertNoteLocked(n)
	}

	v.log.Info().Int("count", len(v.notesByPath)).Msg("vault index rebuild complete")
	return nil
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
func (v *Vault) walkVault(root string, depth int, visited map[string]struct{}, visit func(string) error) error {
	abs, err := filepath.Abs(root)
	if err != nil {
		return errors.Wrapf(err, "resolve path %q", root)
	}
	if _, ok := visited[abs]; ok {
		return nil
	}
	visited[abs] = struct{}{}

	return filepath.WalkDir(abs, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			v.log.Debug().Err(err).Str("path", path).Msg("walk error")
			return nil
		}

		// Resolve symlinks manually to follow them up to a certain depth
		if d.Type()&os.ModeSymlink != 0 {
			if depth >= maxSymlinkDepth {
				v.log.Warn().Str("path", path).Msg("recursion limit reached")
				return nil
			}
			target, err := filepath.EvalSymlinks(path)
			if err != nil {
				return nil
			}
			return v.walkVault(target, depth+1, visited, visit)
		}

		if !d.IsDir() && isMarkdown(path) {
			return visit(path)
		}
		return nil
	})
}

func isMarkdown(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".md" || ext == ".markdown"
}

// parseNote reads the file at path and extracts frontmatter, tags, and
// wikilinks into a Note.
func parseNote(path string) (*Note, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "read note %q", path)
	}

	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	doc, err := markdown.Parse(data, stem)
	if err != nil {
		return nil, errors.Wrapf(err, "parse note %q", path)
	}

	return &Note{
		Path:        path,
		Name:        doc.Name,
		Content:     doc.Content,
		Frontmatter: doc.Frontmatter,
		Tags:        doc.Tags,
		Wikilinks:   doc.Wikilinks,
	}, nil
}
