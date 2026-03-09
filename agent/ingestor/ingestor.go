// Package ingestor processes FileEvents and creates artifact entries.
package ingestor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	artifactv1 "github.com/brightpuddle/clara/gen/artifact/v1"
	"github.com/brightpuddle/clara/agent/watcher"
	"github.com/brightpuddle/clara/internal/artifact"
	"github.com/brightpuddle/clara/internal/db"
	"github.com/brightpuddle/clara/internal/embedding"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const maxFileSize = 1 << 20 // 1 MiB - skip larger files

// Ingestor reads file events, extracts text, generates embeddings,
// and stores artifacts directly in the local database.
type Ingestor struct {
	db          *db.DB
	embedder    *embedding.Client
	concurrency int
	logger      zerolog.Logger
	notifyCh    chan *artifactv1.Artifact // notifies subscribers of new artifacts
}

// New creates a new Ingestor.
func New(database *db.DB, embedder *embedding.Client, concurrency int, logger zerolog.Logger) *Ingestor {
	if concurrency <= 0 {
		concurrency = 4
	}
	return &Ingestor{
		db:          database,
		embedder:    embedder,
		concurrency: concurrency,
		logger:      logger,
		notifyCh:    make(chan *artifactv1.Artifact, 64),
	}
}

// Notifications returns a channel that emits newly ingested artifacts.
func (ing *Ingestor) Notifications() <-chan *artifactv1.Artifact {
	return ing.notifyCh
}

// Run starts the ingestion worker pool consuming from the events channel.
func (ing *Ingestor) Run(ctx context.Context, events <-chan watcher.FileEvent) {
	sem := make(chan struct{}, ing.concurrency)
	var wg sync.WaitGroup

	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case ev, ok := <-events:
			if !ok {
				wg.Wait()
				return
			}
			if ev.Op == watcher.OpRemove || ev.Op == watcher.OpRename {
				continue // skip removes for MVP
			}
			if !isTextFile(ev.Path) {
				continue
			}

			sem <- struct{}{}
			wg.Add(1)
			go func(ev watcher.FileEvent) {
				defer wg.Done()
				defer func() { <-sem }()
				ing.processFile(ctx, ev.Path)
			}(ev)
		}
	}
}

// initialScanRate is the maximum number of files ingested per second during
// the initial directory scan, to reduce startup impact on CPU and disk I/O.
const initialScanRate = 5

// ScanDirs walks all configured directories and ingests existing text files.
// It is rate-limited to initialScanRate files per second and should be called
// in a goroutine after the watcher is started.
func (ing *Ingestor) ScanDirs(ctx context.Context, dirs []string) {
	ticker := time.NewTicker(time.Second / initialScanRate)
	defer ticker.Stop()

	for _, dir := range dirs {
		if err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // skip unreadable entries
			}
			if d.IsDir() {
				return nil
			}
			if !isTextFile(path) {
				return nil
			}
			select {
			case <-ctx.Done():
				return filepath.SkipAll
			case <-ticker.C:
				ing.processFile(ctx, path)
			}
			return nil
		}); err != nil {
			ing.logger.Warn().Err(err).Str("dir", dir).Msg("initial scan walk error")
		}
	}
	ing.logger.Info().Strs("dirs", dirs).Msg("initial directory scan complete")
}

// processFile ingests a single file.
func (ing *Ingestor) processFile(ctx context.Context, path string) {
	log := ing.logger.With().Str("path", path).Logger()

	info, err := os.Stat(path)
	if err != nil {
		return // file may have been deleted already
	}
	if info.Size() > maxFileSize {
		log.Debug().Int64("size", info.Size()).Msg("skipping large file")
		return
	}

	content, err := os.ReadFile(path)
	if err != nil {
		log.Warn().Err(err).Msg("read file")
		return
	}

	text := string(content)
	title := filepath.Base(path)

	// Use absolute path as the stable artifact ID so re-ingesting the same
	// file updates the existing record instead of creating a new row.
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}
	artifactID := "file:" + absPath

	// Determine kind: .md files are notes, everything else is a file.
	kind := artifactv1.ArtifactKind_ARTIFACT_KIND_FILE
	if strings.HasSuffix(strings.ToLower(path), ".md") {
		kind = artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE
		// Use first non-empty, non-frontmatter-delimiter line as title.
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "#"))
			if line != "" && line != "---" {
				title = line
				break
			}
		}
	}

	a := artifact.New(kind, title, text, path, "filesystem")
	a.Id = artifactID
	a.HeatScore = artifact.ComputeHeatScore(a)
	now := time.Now()
	a.CreatedAt = timestamppb.New(info.ModTime())
	a.UpdatedAt = timestamppb.New(now)

	// Store artifact immediately so the UI can display it right away.
	if err := ing.db.UpsertArtifact(ctx, a); err != nil {
		log.Warn().Err(err).Msg("write to db")
		return
	}
	select {
	case ing.notifyCh <- a:
	default:
	}

	// Generate and store embedding asynchronously (best-effort).
	embedCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	vec, err := ing.embedder.Embed(embedCtx, truncate(text, 4096))
	if err != nil {
		log.Warn().Err(err).Msg("embed failed; artifact stored without embedding")
		return
	}

	if err := ing.db.UpsertEmbedding(ctx, a.Id, vec); err != nil {
		log.Warn().Err(err).Msg("store embedding")
		return
	}

	log.Info().Str("id", a.Id).Str("title", title).Float64("heat", a.HeatScore).Msg("ingested artifact")
}

// isTextFile returns true for text-like file extensions.
func isTextFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md", ".txt", ".go", ".py", ".js", ".ts", ".yaml", ".yml",
		".json", ".toml", ".sh", ".env", ".log", ".csv", ".html", ".xml":
		return true
	}
	return false
}

// truncate limits text to maxBytes for embedding.
func truncate(text string, maxBytes int) string {
	if len(text) <= maxBytes {
		return text
	}
	return text[:maxBytes]
}
