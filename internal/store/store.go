// Package store manages Clara's SQLite database. It initialises a CGO-free
// SQLite instance (via ncruces/go-sqlite3) with the sqlite-vec extension loaded
// and provides the Tool implementations for the interpreter to use.
//
// Tool names registered in the registry:
//
//   - "db.query"      — execute arbitrary read SQL and return rows as JSON
//   - "db.exec"       — execute a write SQL statement (no result rows)
//   - "db.vec_search" — perform a vec0 vector similarity search
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"

	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"

	// Side-effect import: loads the ncruces SQLite WASM build with sqlite-vec.
	_ "github.com/asg017/sqlite-vec-go-bindings/ncruces"
	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// init enables the WASM threads feature required by the sqlite-vec binary.
// The sqlite-vec WASM build uses atomic memory operations which need the
// threads proposal enabled in wazero.
func init() {
	features := api.CoreFeaturesV2 | experimental.CoreFeaturesThreads
	sqlite3.RuntimeConfig = wazero.NewRuntimeConfig().WithCoreFeatures(features)
}


type Store struct {
	db  *sql.DB
	log zerolog.Logger
}

// Open opens or creates the SQLite database at dbPath.
func Open(dbPath string, log zerolog.Logger) (*Store, error) {
	db, err := driver.Open(dbPath, nil)
	if err != nil {
		return nil, errors.Wrap(err, "open sqlite database")
	}

	s := &Store{db: db, log: log.With().Str("component", "store").Logger()}

	if err := s.migrate(); err != nil {
		db.Close()
		return nil, errors.Wrap(err, "run migrations")
	}
	return s, nil
}

// OpenMemory opens an in-memory SQLite database for testing.
func OpenMemory(log zerolog.Logger) (*Store, error) {
	return Open(":memory:", log)
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying *sql.DB for direct use where needed.
func (s *Store) DB() *sql.DB {
	return s.db
}

// migrate creates the base schema if it does not exist.
// If the legacy blueprint_runs table exists, its data is migrated to intent_runs.
func (s *Store) migrate() error {
	// Create new schema.
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS intent_runs (
			id         TEXT    NOT NULL,
			intent_id  TEXT    NOT NULL,
			state      TEXT    NOT NULL,
			mem_json   TEXT    NOT NULL DEFAULT '{}',
			started_at INTEGER NOT NULL DEFAULT (unixepoch()),
			updated_at INTEGER NOT NULL DEFAULT (unixepoch()),
			PRIMARY KEY (id)
		);

		CREATE INDEX IF NOT EXISTS idx_intent_runs_intent_id
			ON intent_runs(intent_id);
	`)
	if err != nil {
		return errors.Wrap(err, "create schema")
	}

	// Migrate data from the legacy blueprint_runs table if it exists.
	_, _ = s.db.Exec(`
		INSERT OR IGNORE INTO intent_runs (id, intent_id, state, mem_json, started_at, updated_at)
		SELECT id, blueprint_id, state, mem_json, started_at, updated_at FROM blueprint_runs
	`)
	_, _ = s.db.Exec(`DROP TABLE IF EXISTS blueprint_runs`)

	return nil
}

// QueryTool returns a registry Tool that executes a read SQL query.
// Args: sql (string), params ([]any, optional)
func (s *Store) QueryTool() func(ctx context.Context, args map[string]any) (any, error) {
	return func(ctx context.Context, args map[string]any) (any, error) {
		query, ok := args["sql"].(string)
		if !ok || query == "" {
			return nil, errors.New("db.query: 'sql' argument is required")
		}
		params := toSlice(args["params"])

		rows, err := s.db.QueryContext(ctx, query, params...)
		if err != nil {
			return nil, errors.Wrap(err, "db.query")
		}
		defer rows.Close()

		return scanRows(rows)
	}
}

// ExecTool returns a registry Tool that executes a write SQL statement.
// Args: sql (string), params ([]any, optional)
func (s *Store) ExecTool() func(ctx context.Context, args map[string]any) (any, error) {
	return func(ctx context.Context, args map[string]any) (any, error) {
		query, ok := args["sql"].(string)
		if !ok || query == "" {
			return nil, errors.New("db.exec: 'sql' argument is required")
		}
		params := toSlice(args["params"])

		result, err := s.db.ExecContext(ctx, query, params...)
		if err != nil {
			return nil, errors.Wrap(err, "db.exec")
		}
		affected, _ := result.RowsAffected()
		return map[string]any{"rows_affected": affected}, nil
	}
}

// VecSearchTool returns a registry Tool that performs a vector similarity search
// using the sqlite-vec vec0 virtual table.
//
// Required args:
//   - table  (string): vec0 virtual table name
//   - vector ([]float32 or []byte): query vector
//   - limit  (int): max results (default 10)
//
// Optional args:
//   - min_score (float64): minimum similarity score threshold
func (s *Store) VecSearchTool() func(ctx context.Context, args map[string]any) (any, error) {
	return func(ctx context.Context, args map[string]any) (any, error) {
		table, ok := args["table"].(string)
		if !ok || table == "" {
			return nil, errors.New("db.vec_search: 'table' argument is required")
		}

		vectorBytes, err := encodeVector(args["vector"])
		if err != nil {
			return nil, errors.Wrap(err, "db.vec_search: encode vector")
		}

		limit := 10
		if l, ok := args["limit"].(int); ok && l > 0 {
			limit = l
		} else if l, ok := args["limit"].(float64); ok && l > 0 {
			limit = int(l)
		}

		// sqlite-vec knn query: SELECT rowid, distance FROM <table>
		// WHERE <embedding_col> MATCH ? AND k = ?
		// We use a standard pattern that works for vec0 tables.
		query := fmt.Sprintf(
			`SELECT rowid, distance FROM %s WHERE embedding MATCH ? AND k = ?`, table,
		)
		rows, err := s.db.QueryContext(ctx, query, vectorBytes, limit)
		if err != nil {
			return nil, errors.Wrapf(err, "db.vec_search: query table %q", table)
		}
		defer rows.Close()

		type searchResult struct {
			RowID    int64   `json:"rowid"`
			Distance float64 `json:"distance"`
		}

		minScore, _ := args["min_score"].(float64)
		var results []searchResult
		for rows.Next() {
			var r searchResult
			if err := rows.Scan(&r.RowID, &r.Distance); err != nil {
				return nil, errors.Wrap(err, "db.vec_search: scan row")
			}
			// sqlite-vec returns distance (lower = more similar). A min_score
			// here acts as a max-distance filter when provided.
			if minScore > 0 && r.Distance > minScore {
				continue
			}
			results = append(results, r)
		}
		if err := rows.Err(); err != nil {
			return nil, errors.Wrap(err, "db.vec_search: rows error")
		}
		return results, nil
	}
}

// SaveRunState persists the current execution state of an intent run to
// survive daemon restarts.
func (s *Store) SaveRunState(ctx context.Context, runID, intentID, state string, mem map[string]any) error {
	memJSON, err := json.Marshal(mem)
	if err != nil {
		return errors.Wrap(err, "marshal mem")
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO intent_runs (id, intent_id, state, mem_json, updated_at)
		VALUES (?, ?, ?, ?, unixepoch())
		ON CONFLICT(id) DO UPDATE SET
			state      = excluded.state,
			mem_json   = excluded.mem_json,
			updated_at = unixepoch()
	`, runID, intentID, state, string(memJSON))
	return errors.Wrap(err, "save run state")
}

// LoadRunState loads a previously-saved run state for resumption after restart.
// Returns ("", nil, nil) if the run does not exist.
func (s *Store) LoadRunState(ctx context.Context, runID string) (state string, mem map[string]any, err error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT state, mem_json FROM intent_runs WHERE id = ?`, runID,
	)
	var memJSON string
	if err = row.Scan(&state, &memJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil, nil
		}
		return "", nil, errors.Wrap(err, "load run state")
	}
	if err = json.Unmarshal([]byte(memJSON), &mem); err != nil {
		return "", nil, errors.Wrap(err, "unmarshal mem json")
	}
	return state, mem, nil
}

// scanRows converts sql.Rows into a []map[string]any for easy JSON encoding.
func scanRows(rows *sql.Rows) ([]map[string]any, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, errors.Wrap(err, "get columns")
	}

	var results []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, errors.Wrap(err, "scan row")
		}
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			row[col] = vals[i]
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows error")
	}
	return results, nil
}

// toSlice converts an args value to []any for use as SQL parameters.
func toSlice(v any) []any {
	if v == nil {
		return nil
	}
	if s, ok := v.([]any); ok {
		return s
	}
	return []any{v}
}

// encodeVector converts a vector argument ([]float32 or []byte) to bytes
// suitable for sqlite-vec MATCH queries.
func encodeVector(v any) ([]byte, error) {
	switch val := v.(type) {
	case []byte:
		return val, nil
	case []float32:
		// Encode as little-endian IEEE 754 floats (sqlite-vec format).
		buf := make([]byte, len(val)*4)
		for i, f := range val {
			bits := math.Float32bits(f)
			buf[i*4] = byte(bits)
			buf[i*4+1] = byte(bits >> 8)
			buf[i*4+2] = byte(bits >> 16)
			buf[i*4+3] = byte(bits >> 24)
		}
		return buf, nil
	case nil:
		return nil, errors.New("vector is required")
	default:
		return nil, errors.Newf("unsupported vector type %T", v)
	}
}
