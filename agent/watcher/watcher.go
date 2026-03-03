package watcher

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/brightpuddle/clara/agent/ingest"
	"github.com/brightpuddle/clara/pb"
	"github.com/fsnotify/fsnotify"
)

// Watcher monitors a directory for markdown file changes and ingests them.
type Watcher struct {
	dir      string
	client   *ingest.Client
	OnIngest func() // called after each successful file ingest (may be nil)
}

func New(dir string, client *ingest.Client) (*Watcher, error) {
	return &Watcher{dir: dir, client: client}, nil
}

// Run starts the FSNotify watch loop. Blocks until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) error {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer fw.Close()

	// Watch the directory and all subdirectories, following symlinks safely.
	if err := walkFollowSymlinks(w.dir, func(path string, d os.DirEntry) error {
		if d.IsDir() {
			return fw.Add(path)
		}
		return nil
	}); err != nil {
		return err
	}

	// Debounce: collect events and flush after a short pause
	pending := make(map[string]struct{})
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil

		case event, ok := <-fw.Events:
			if !ok {
				return nil
			}
			if !isMarkdown(event.Name) {
				continue
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				pending[event.Name] = struct{}{}
			}

		case err, ok := <-fw.Errors:
			if !ok {
				return nil
			}
			slog.Warn("watcher error", "err", err)

		case <-ticker.C:
			for path := range pending {
				delete(pending, path)
				if err := w.ingestFile(ctx, path); err != nil {
					slog.Warn("ingest error", "path", path, "err", err)
				}
			}
		}
	}
}

// Scan does a one-time walk of the notes directory and ingests all .md files.
func (w *Watcher) Scan(ctx context.Context) error {
	return walkFollowSymlinks(w.dir, func(path string, d os.DirEntry) error {
		if d.IsDir() || !isMarkdown(path) {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return w.ingestFile(ctx, path)
	})
}

// walkFollowSymlinks walks root recursively, following symlinked directories.
// It tracks real (resolved) directory paths to prevent infinite loops from
// circular symlinks.
func walkFollowSymlinks(root string, fn func(path string, d os.DirEntry) error) error {
	// Resolve root itself in case it is a symlink.
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	visited := map[string]struct{}{realRoot: {}}
	return walkDir(realRoot, visited, fn)
}

func walkDir(dir string, visited map[string]struct{}, fn func(string, os.DirEntry) error) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		slog.Warn("walkDir: read error", "dir", dir, "err", err)
		return nil // skip unreadable directories
	}

	// Call fn for the directory entry itself (synthetic, using the dir path).
	dirEntry := dirEntryForPath(dir)
	if dirEntry != nil {
		if err := fn(dir, dirEntry); err != nil {
			return err
		}
	}

	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())

		if entry.Type()&os.ModeSymlink != 0 {
			// Resolve the symlink target.
			real, resolveErr := filepath.EvalSymlinks(path)
			if resolveErr != nil {
				slog.Warn("walkDir: cannot resolve symlink", "path", path, "err", resolveErr)
				continue
			}
			info, statErr := os.Stat(real)
			if statErr != nil {
				continue
			}
			if info.IsDir() {
				// Guard against cycles.
				if _, seen := visited[real]; seen {
					slog.Debug("walkDir: skipping cyclic symlink", "path", path, "real", real)
					continue
				}
				visited[real] = struct{}{}
				if err := walkDir(real, visited, fn); err != nil {
					return err
				}
			} else {
				// Symlink to a file — treat as a regular file entry.
				if err := fn(path, entry); err != nil {
					return err
				}
			}
			continue
		}

		if entry.IsDir() {
			real, resolveErr := filepath.EvalSymlinks(path)
			if resolveErr != nil {
				real = path
			}
			if _, seen := visited[real]; seen {
				continue
			}
			visited[real] = struct{}{}
			if err := walkDir(path, visited, fn); err != nil {
				return err
			}
		} else {
			if err := fn(path, entry); err != nil {
				return err
			}
		}
	}
	return nil
}

// dirEntryForPath returns a synthetic os.DirEntry representing a directory.
// Returns nil on error (directory will still be descended into).
func dirEntryForPath(path string) os.DirEntry {
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return nil
	}
	name := filepath.Base(path)
	for _, e := range entries {
		if e.Name() == name {
			return e
		}
	}
	return nil
}

func (w *Watcher) ingestFile(ctx context.Context, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	title := titleFromPath(path)
	req := &pb.IngestRequest{
		DocumentId: path, // use absolute path as stable ID
		Path:       path,
		Title:      title,
		Content:    string(data),
		ModifiedAt: info.ModTime().Unix(),
	}

	slog.Debug("ingesting", "path", path)
	if err := w.client.IngestNote(ctx, req); err != nil {
		return err
	}
	if w.OnIngest != nil {
		w.OnIngest()
	}
	return nil
}

func isMarkdown(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".md" || ext == ".markdown"
}

func titleFromPath(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
