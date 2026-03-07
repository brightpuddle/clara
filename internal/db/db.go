// Package db provides SQLite database initialization and schema management.
package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"

	"github.com/cockroachdb/errors"
	// ncruces/go-sqlite3 driver registers itself under "sqlite3"
	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// DB wraps a *sql.DB with Clara-specific helpers.
type DB struct {
	*sql.DB
}

// Open opens (or creates) the SQLite database at the given path and runs
// schema migrations.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, errors.Wrap(err, "create db directory")
	}

	conn, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, errors.Wrap(err, "open sqlite3")
	}

	// Single writer; multiple readers OK.
	conn.SetMaxOpenConns(1)

	db := &DB{conn}
	if err := db.migrate(context.Background()); err != nil {
		_ = conn.Close()
		return nil, errors.Wrap(err, "migrate")
	}
	return db, nil
}

// migrate runs all schema migrations idempotently.
func (db *DB) migrate(ctx context.Context) error {
	stmts := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA foreign_keys=ON`,

		// Core artifacts table
		`CREATE TABLE IF NOT EXISTS artifacts (
			id          TEXT PRIMARY KEY,
			kind        INTEGER NOT NULL DEFAULT 0,
			title       TEXT    NOT NULL DEFAULT '',
			content     TEXT    NOT NULL DEFAULT '',
			source_path TEXT    NOT NULL DEFAULT '',
			source_app  TEXT    NOT NULL DEFAULT '',
			heat_score  REAL    NOT NULL DEFAULT 0.0,
			done        INTEGER NOT NULL DEFAULT 0,
			tags        TEXT    NOT NULL DEFAULT '[]',   -- JSON array
			metadata    TEXT    NOT NULL DEFAULT '{}',   -- JSON object
			created_at  INTEGER NOT NULL DEFAULT (unixepoch()),
			updated_at  INTEGER NOT NULL DEFAULT (unixepoch()),
			due_at      INTEGER -- nullable unix timestamp
		)`,

		`CREATE INDEX IF NOT EXISTS idx_artifacts_kind       ON artifacts(kind)`,
		`CREATE INDEX IF NOT EXISTS idx_artifacts_heat_score ON artifacts(heat_score DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_artifacts_done       ON artifacts(done)`,

		// Embeddings table: stores float32 vectors as BLOBs.
		// Compatible with sqlite-vec schema conventions.
		`CREATE TABLE IF NOT EXISTS artifact_embeddings (
			artifact_id TEXT PRIMARY KEY REFERENCES artifacts(id) ON DELETE CASCADE,
			embedding   BLOB NOT NULL,   -- little-endian float32 array
			dim         INTEGER NOT NULL -- number of dimensions (768 for nomic-embed-text)
		)`,

		// Operations log for reversibility.
		`CREATE TABLE IF NOT EXISTS ops_log (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			op_type     TEXT    NOT NULL,  -- 'mark_done', 'move_file', 'tag', etc.
			artifact_id TEXT,
			payload     TEXT    NOT NULL DEFAULT '{}', -- JSON
			created_at  INTEGER NOT NULL DEFAULT (unixepoch()),
			reversed_at INTEGER -- nullable; set when the operation is undone
		)`,

		`CREATE INDEX IF NOT EXISTS idx_ops_log_artifact_id ON ops_log(artifact_id)`,
		`CREATE INDEX IF NOT EXISTS idx_ops_log_created_at  ON ops_log(created_at DESC)`,
	}

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return errors.Wrapf(err, "exec: %s", stmt[:min(60, len(stmt))])
		}
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
