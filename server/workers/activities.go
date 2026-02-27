package workers

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/brightpuddle/clara/server/db"
)

const (
	minSimilarity = 0.75
	maxCandidates = 10
)

// wikilinkRe matches [[any text]] patterns in markdown.
var wikilinkRe = regexp.MustCompile(`\[\[([^\]]+)\]\]`)

// Activities holds dependencies for Temporal activities.
type Activities struct {
	db *db.DB
}

func NewActivities(database *db.DB) *Activities {
	return &Activities{db: database}
}

// FindSimilarDocs queries pgvector for documents similar to the given one.
func (a *Activities) FindSimilarDocs(ctx context.Context, documentID string) ([]db.SimilarDoc, error) {
	return a.db.FindSimilar(ctx, documentID, maxCandidates, minSimilarity)
}

// CreateSuggestions creates suggestion records for document pairs that don't
// already have a wikilink between them.
func (a *Activities) CreateSuggestions(ctx context.Context, documentID string, candidates []db.SimilarDoc) error {
	// Load source document to check existing links
	sourceContent, err := a.getDocContent(ctx, documentID)
	if err != nil {
		return fmt.Errorf("get source content: %w", err)
	}
	existingLinks := extractWikilinks(sourceContent)

	for _, candidate := range candidates {
		// Skip if source already links to this target
		if existingLinks[normalizeTitle(candidate.Title)] {
			continue
		}

		context := fmt.Sprintf(
			"Similar content found (%.0f%% match): \"%s\"",
			candidate.Similarity*100,
			truncate(candidate.ChunkContent, 120),
		)

		if err := a.db.UpsertSuggestion(ctx, documentID, candidate.DocumentID,
			candidate.Similarity, context); err != nil {
			return fmt.Errorf("upsert suggestion %s -> %s: %w", documentID, candidate.DocumentID, err)
		}
	}
	return nil
}

func (a *Activities) getDocContent(ctx context.Context, documentID string) (string, error) {
	var content string
	err := a.db.QueryRow(ctx,
		`SELECT content FROM documents WHERE id = $1`, documentID,
	).Scan(&content)
	return content, err
}

func extractWikilinks(content string) map[string]bool {
	links := make(map[string]bool)
	for _, match := range wikilinkRe.FindAllStringSubmatch(content, -1) {
		links[normalizeTitle(match[1])] = true
	}
	return links
}

func normalizeTitle(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
