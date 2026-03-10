// Package db - artifacts.go provides CRUD operations for artifacts.
package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/cockroachdb/errors"

	artifactv1 "github.com/brightpuddle/clara/gen/artifact/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// UpsertArtifact inserts or replaces an artifact record.
func (db *DB) UpsertArtifact(ctx context.Context, a *artifactv1.Artifact) error {
	tagsJSON, err := json.Marshal(a.Tags)
	if err != nil {
		return errors.Wrap(err, "marshal tags")
	}
	metaJSON, err := json.Marshal(a.Metadata)
	if err != nil {
		return errors.Wrap(err, "marshal metadata")
	}

	var dueAt *int64
	if a.DueAt != nil {
		ts := a.DueAt.AsTime().Unix()
		dueAt = &ts
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO artifacts
			(id, kind, title, content, source_path, source_app, heat_score, done, tags, metadata, created_at, updated_at, due_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			kind        = excluded.kind,
			title       = excluded.title,
			content     = excluded.content,
			source_path = excluded.source_path,
			source_app  = excluded.source_app,
			heat_score  = excluded.heat_score,
			tags        = excluded.tags,
			metadata    = excluded.metadata,
			updated_at  = excluded.updated_at,
			due_at      = excluded.due_at
	`,
		a.Id,
		int(a.Kind),
		a.Title,
		a.Content,
		a.SourcePath,
		a.SourceApp,
		a.HeatScore,
		boolToInt(a.Done),
		string(tagsJSON),
		string(metaJSON),
		timeOrNow(a.CreatedAt),
		timeOrNow(a.UpdatedAt),
		dueAt,
	)
	return errors.Wrap(err, "upsert artifact")
}

// GetArtifact retrieves a single artifact by ID.
func (db *DB) GetArtifact(ctx context.Context, id string) (*artifactv1.Artifact, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, kind, title, content, source_path, source_app, heat_score, done, tags, metadata, created_at, updated_at, due_at
		FROM artifacts WHERE id = ?
	`, id)
	return scanArtifact(row)
}

// ListArtifacts returns artifacts sorted by heat_score descending.
// kinds is an optional filter; if empty, all kinds are returned.
func (db *DB) ListArtifacts(ctx context.Context, limit, offset int, kinds []artifactv1.ArtifactKind) ([]*artifactv1.Artifact, error) {
	query := `
		SELECT id, kind, title, content, source_path, source_app, heat_score, done, tags, metadata, created_at, updated_at, due_at
		FROM artifacts
		WHERE done = 0
	`
	args := []any{}

	if len(kinds) > 0 {
		query += ` AND kind IN (`
		for i, k := range kinds {
			if i > 0 {
				query += ","
			}
			query += "?"
			args = append(args, int(k))
		}
		query += ")"
	}

	query += " ORDER BY heat_score DESC"

	if limit > 0 {
		query += " LIMIT ? OFFSET ?"
		args = append(args, limit, offset)
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "list artifacts query")
	}
	defer rows.Close()

	var artifacts []*artifactv1.Artifact
	for rows.Next() {
		a, err := scanArtifactRow(rows)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, a)
	}
	return artifacts, errors.Wrap(rows.Err(), "list artifacts rows")
}

// MarkDone marks an artifact as done.
func (db *DB) MarkDone(ctx context.Context, id string) error {
	_, err := db.ExecContext(ctx, `
		UPDATE artifacts SET done = 1, updated_at = ? WHERE id = ?
	`, time.Now().Unix(), id)
	return errors.Wrap(err, "mark done")
}

// SearchArtifacts performs a simple full-text LIKE search across title and content.
func (db *DB) SearchArtifacts(ctx context.Context, query string, limit int, kinds []artifactv1.ArtifactKind) ([]*artifactv1.Artifact, error) {
	like := "%" + query + "%"
	baseQuery := `
		SELECT id, kind, title, content, source_path, source_app, heat_score, done, tags, metadata, created_at, updated_at, due_at
		FROM artifacts
		WHERE done = 0 AND (title LIKE ? OR content LIKE ?)
	`
	args := []any{like, like}

	if len(kinds) > 0 {
		baseQuery += " AND kind IN ("
		for i, k := range kinds {
			if i > 0 {
				baseQuery += ","
			}
			baseQuery += "?"
			args = append(args, int(k))
		}
		baseQuery += ")"
	}

	baseQuery += " ORDER BY heat_score DESC LIMIT ?"
	if limit <= 0 {
		limit = 20
	}
	args = append(args, limit)

	rows, err := db.QueryContext(ctx, baseQuery, args...)
	if err != nil {
		return nil, errors.Wrap(err, "search artifacts query")
	}
	defer rows.Close()

	var artifacts []*artifactv1.Artifact
	for rows.Next() {
		a, err := scanArtifactRow(rows)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, a)
	}
	return artifacts, errors.Wrap(rows.Err(), "search artifacts rows")
}

// LogOp inserts a record into the ops_log table.
func (db *DB) LogOp(ctx context.Context, opType, artifactID string, payload any) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return errors.Wrap(err, "marshal op payload")
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO ops_log (op_type, artifact_id, payload) VALUES (?, ?, ?)
	`, opType, artifactID, string(payloadJSON))
	return errors.Wrap(err, "log op")
}

// scanArtifact scans a single *sql.Row into an Artifact proto.
func scanArtifact(row *sql.Row) (*artifactv1.Artifact, error) {
	var (
		id, title, content, sourcePath, sourceApp string
		kind                                      int
		heatScore                                 float64
		done                                      int
		tagsJSON, metaJSON                        string
		createdAt, updatedAt                      int64
		dueAt                                     sql.NullInt64
	)
	err := row.Scan(&id, &kind, &title, &content, &sourcePath, &sourceApp,
		&heatScore, &done, &tagsJSON, &metaJSON, &createdAt, &updatedAt, &dueAt)
	if err != nil {
		return nil, errors.Wrap(err, "scan artifact")
	}
	return buildArtifact(id, kind, title, content, sourcePath, sourceApp, heatScore, done, tagsJSON, metaJSON, createdAt, updatedAt, dueAt)
}

// scanArtifactRow scans a *sql.Rows into an Artifact proto.
func scanArtifactRow(rows *sql.Rows) (*artifactv1.Artifact, error) {
	var (
		id, title, content, sourcePath, sourceApp string
		kind                                      int
		heatScore                                 float64
		done                                      int
		tagsJSON, metaJSON                        string
		createdAt, updatedAt                      int64
		dueAt                                     sql.NullInt64
	)
	err := rows.Scan(&id, &kind, &title, &content, &sourcePath, &sourceApp,
		&heatScore, &done, &tagsJSON, &metaJSON, &createdAt, &updatedAt, &dueAt)
	if err != nil {
		return nil, errors.Wrap(err, "scan artifact row")
	}
	return buildArtifact(id, kind, title, content, sourcePath, sourceApp, heatScore, done, tagsJSON, metaJSON, createdAt, updatedAt, dueAt)
}

func buildArtifact(id string, kind int, title, content, sourcePath, sourceApp string,
	heatScore float64, done int, tagsJSON, metaJSON string, createdAt, updatedAt int64, dueAt sql.NullInt64,
) (*artifactv1.Artifact, error) {
	var tags []string
	if err := json.Unmarshal([]byte(tagsJSON), &tags); err != nil {
		tags = nil
	}
	var meta map[string]string
	if err := json.Unmarshal([]byte(metaJSON), &meta); err != nil {
		meta = nil
	}

	a := &artifactv1.Artifact{
		Id:         id,
		Kind:       artifactv1.ArtifactKind(kind),
		Title:      title,
		Content:    content,
		SourcePath: sourcePath,
		SourceApp:  sourceApp,
		HeatScore:  heatScore,
		Done:       done == 1,
		Tags:       tags,
		Metadata:   meta,
		CreatedAt:  timestamppb.New(time.Unix(createdAt, 0)),
		UpdatedAt:  timestamppb.New(time.Unix(updatedAt, 0)),
	}
	if dueAt.Valid {
		a.DueAt = timestamppb.New(time.Unix(dueAt.Int64, 0))
	}
	return a, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func timeOrNow(ts *timestamppb.Timestamp) int64 {
	if ts == nil {
		return time.Now().Unix()
	}
	return ts.AsTime().Unix()
}
