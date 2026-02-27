package db

import (
	"context"
	"crypto/sha256"
	"embed"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

//go:embed schema.sql
var schemaFS embed.FS

// DB wraps a pgxpool and exposes typed query methods.
type DB struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, dsn string) (*DB, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("db ping: %w", err)
	}
	return &DB{pool: pool}, nil
}

// Migrate runs the embedded schema SQL (idempotent via IF NOT EXISTS).
func (d *DB) Migrate(ctx context.Context) error {
	sql, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		return err
	}
	_, err = d.pool.Exec(ctx, string(sql))
	return err
}

func (d *DB) Close() { d.pool.Close() }

// QueryRow exposes a single-row query for use outside this package.
func (d *DB) QueryRow(ctx context.Context, sql string, args ...any) interface{ Scan(...any) error } {
	return d.pool.QueryRow(ctx, sql, args...)
}


// ---- Document ---------------------------------------------------------------

type Document struct {
	ID         string
	Path       string
	Title      string
	Content    string
	Checksum   string
	ModifiedAt time.Time
}

func Checksum(content string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
}

// UpsertDocument inserts or updates a document. Returns true if the content
// changed (i.e. re-embedding is needed).
func (d *DB) UpsertDocument(ctx context.Context, doc Document) (changed bool, err error) {
	var existing string
	err = d.pool.QueryRow(ctx,
		`SELECT checksum FROM documents WHERE id = $1`, doc.ID,
	).Scan(&existing)

	if err == nil && existing == doc.Checksum {
		return false, nil // unchanged
	}

	_, err = d.pool.Exec(ctx, `
		INSERT INTO documents (id, path, title, content, checksum, modified_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (id) DO UPDATE
		  SET path = EXCLUDED.path,
		      title = EXCLUDED.title,
		      content = EXCLUDED.content,
		      checksum = EXCLUDED.checksum,
		      modified_at = EXCLUDED.modified_at,
		      updated_at = now()`,
		doc.ID, doc.Path, doc.Title, doc.Content, doc.Checksum, doc.ModifiedAt,
	)
	return err == nil, err
}

// ---- Chunks -----------------------------------------------------------------

// DeleteChunks removes all chunks for a document (called before re-embedding).
func (d *DB) DeleteChunks(ctx context.Context, documentID string) error {
	_, err := d.pool.Exec(ctx, `DELETE FROM chunks WHERE document_id = $1`, documentID)
	return err
}

// InsertChunk stores a single chunk with its embedding.
func (d *DB) InsertChunk(ctx context.Context, documentID string, index int, content string, embedding []float32) error {
	_, err := d.pool.Exec(ctx, `
		INSERT INTO chunks (document_id, chunk_index, content, embedding)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (document_id, chunk_index) DO UPDATE
		  SET content = EXCLUDED.content, embedding = EXCLUDED.embedding`,
		documentID, index, content, pgvector.NewVector(embedding),
	)
	return err
}

// ---- Similarity search ------------------------------------------------------

type SimilarDoc struct {
	DocumentID string
	Path       string
	Title      string
	Similarity float64
	ChunkContent string // the matching chunk text (for context)
}

// FindSimilar returns documents whose chunks are most similar to those of
// the given document, excluding the document itself.
func (d *DB) FindSimilar(ctx context.Context, documentID string, limit int, minSimilarity float64) ([]SimilarDoc, error) {
	rows, err := d.pool.Query(ctx, `
		WITH source_chunks AS (
			SELECT embedding FROM chunks WHERE document_id = $1
		),
		scored AS (
			SELECT
				c.document_id,
				MAX(1 - (c.embedding <=> sc.embedding)) AS similarity,
				(array_agg(c.content ORDER BY (1 - (c.embedding <=> sc.embedding)) DESC))[1] AS chunk_content
			FROM chunks c
			CROSS JOIN source_chunks sc
			WHERE c.document_id != $1
			GROUP BY c.document_id
		)
		SELECT s.document_id, d.path, d.title, s.similarity, s.chunk_content
		FROM scored s
		JOIN documents d ON d.id = s.document_id
		WHERE s.similarity >= $3
		ORDER BY s.similarity DESC
		LIMIT $2`,
		documentID, limit, minSimilarity,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SimilarDoc
	for rows.Next() {
		var r SimilarDoc
		if err := rows.Scan(&r.DocumentID, &r.Path, &r.Title, &r.Similarity, &r.ChunkContent); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// ---- Suggestions ------------------------------------------------------------

type Suggestion struct {
	ID           int64
	Type         string
	SourceDocID  string
	TargetDocID  string
	SourcePath   string
	TargetTitle  string
	Similarity   float64
	Context      string
	Status       string
	CreatedAt    time.Time
}

func (d *DB) UpsertSuggestion(ctx context.Context, sourceID, targetID string, similarity float64, context string) error {
	_, err := d.pool.Exec(ctx, `
		INSERT INTO suggestions (source_doc_id, target_doc_id, similarity, context)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (source_doc_id, target_doc_id) DO NOTHING`,
		sourceID, targetID, similarity, context,
	)
	return err
}

func (d *DB) ListSuggestions(ctx context.Context, status string) ([]Suggestion, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT s.id, s.type, s.source_doc_id, s.target_doc_id,
		       src.path, tgt.title, s.similarity, s.context, s.status, s.created_at
		FROM suggestions s
		JOIN documents src ON src.id = s.source_doc_id
		JOIN documents tgt ON tgt.id = s.target_doc_id
		WHERE s.status = $1
		ORDER BY s.similarity DESC, s.created_at DESC`,
		status,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var suggestions []Suggestion
	for rows.Next() {
		var s Suggestion
		if err := rows.Scan(&s.ID, &s.Type, &s.SourceDocID, &s.TargetDocID,
			&s.SourcePath, &s.TargetTitle, &s.Similarity, &s.Context, &s.Status, &s.CreatedAt); err != nil {
			return nil, err
		}
		suggestions = append(suggestions, s)
	}
	return suggestions, rows.Err()
}

func (d *DB) UpdateSuggestionStatus(ctx context.Context, id int64, status string) error {
	_, err := d.pool.Exec(ctx, `
		UPDATE suggestions SET status = $2, updated_at = now() WHERE id = $1`,
		id, status,
	)
	return err
}

// GetApprovedSuggestions returns suggestions ready for the agent to act on.
func (d *DB) GetApprovedSuggestions(ctx context.Context) ([]Suggestion, error) {
	return d.ListSuggestions(ctx, "approved")
}

func (d *DB) MarkSuggestionApplied(ctx context.Context, id int64) error {
	return d.UpdateSuggestionStatus(ctx, id, "applied")
}
