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
	dir    string
	client *ingest.Client
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

	// Watch the directory and all subdirectories
	if err := filepath.WalkDir(w.dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors (e.g. permission denied)
		}
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
	return filepath.WalkDir(w.dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !isMarkdown(path) {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return w.ingestFile(ctx, path)
	})
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
	return w.client.IngestNote(ctx, req)
}

func isMarkdown(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".md" || ext == ".markdown"
}

func titleFromPath(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
