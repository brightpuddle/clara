// Package db provides the built-in SQLite integration as an in-process tool.
// It registers query, exec, vec_search, and stage_rows tools.
package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"

	"github.com/brightpuddle/clara/internal/builtin/pathutil"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/driver"
	"github.com/rs/zerolog"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"

	_ "github.com/asg017/sqlite-vec-go-bindings/ncruces"
	_ "github.com/ncruces/go-sqlite3/embed"
)

const description = "Built-in SQLite: query, exec, and vector search."

func init() {
	features := api.CoreFeaturesV2 | experimental.CoreFeaturesThreads
	sqlite3.RuntimeConfig = wazero.NewRuntimeConfig().WithCoreFeatures(features)
}

var sqliteIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// service wraps an open SQLite database and provides tool handlers.
type service struct {
	db *sql.DB
}

// open creates a new service backed by the SQLite database at path.
// Pass an empty string or ":memory:" for an in-memory database.
func open(path string) (*service, error) {
	resolved := pathutil.Resolve(path)
	if resolved == "" {
		resolved = ":memory:"
	}
	if resolved != ":memory:" {
		dir := filepath.Dir(resolved)
		if dir != "." {
			if err := os.MkdirAll(dir, 0o750); err != nil {
				return nil, errors.Wrapf(err, "create db directory for %q", resolved)
			}
		}
	}

	db, err := driver.Open(resolved, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "open sqlite database %q", resolved)
	}
	return &service{db: db}, nil
}

func (s *service) close() error {
	return s.db.Close()
}

// Register adds all db tools into reg under the "db" namespace.
// cfg may contain a "path" key specifying the SQLite database file.
func Register(
	ctx context.Context,
	cfg map[string]any,
	reg *registry.Registry,
	log zerolog.Logger,
) error {
	log.Debug().Msg("registering db builtin")

	var path string
	if cfg != nil {
		path, _ = cfg["path"].(string)
	}

	svc, err := open(path)
	if err != nil {
		return errors.Wrap(err, "db builtin: open database")
	}

	// Close the database when the context is cancelled.
	go func() {
		<-ctx.Done()
		if closeErr := svc.close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("db builtin: error closing database")
		}
	}()

	reg.RegisterNamespaceDescription("db", description)

	tools := []struct {
		spec    mcp.Tool
		handler func(context.Context, map[string]any) (any, error)
	}{
		{
			mcp.NewTool("db.query",
				mcp.WithDescription("Execute a SQL query and return the results."),
				mcp.WithString(
					"sql",
					mcp.Required(),
					mcp.Description("SQL query text to execute."),
				),
				mcp.WithArray(
					"params",
					mcp.Description("Optional positional parameters bound to the SQL query."),
				),
			),
			svc.handleQuery,
		},
		{
			mcp.NewTool("db.exec",
				mcp.WithDescription(
					"Execute a SQL statement and return the number of affected rows.",
				),
				mcp.WithString(
					"sql",
					mcp.Required(),
					mcp.Description("SQL statement text to execute."),
				),
				mcp.WithArray(
					"params",
					mcp.Description("Optional positional parameters bound to the SQL statement."),
				),
			),
			svc.handleExec,
		},
		{
			mcp.NewTool("db.vec_search",
				mcp.WithDescription(
					"Perform a vector similarity search over a sqlite-vec table.",
				),
				mcp.WithString(
					"table",
					mcp.Required(),
					mcp.Description("Name of the vec0 virtual table to query."),
				),
				mcp.WithArray(
					"vector",
					mcp.Required(),
					mcp.Description(
						"Query vector represented as a JSON array of floats or raw bytes.",
					),
				),
				mcp.WithNumber(
					"limit",
					mcp.Description("Maximum number of matches to return. Defaults to 10."),
				),
				mcp.WithNumber(
					"min_score",
					mcp.Description("Optional maximum distance threshold for returned matches."),
				),
			),
			svc.handleVecSearch,
		},
		{
			mcp.NewTool("db.stage_rows",
				mcp.WithDescription(
					"Stage structured rows into a SQLite table with a single json column for later SQL processing.",
				),
				mcp.WithString(
					"table",
					mcp.Required(),
					mcp.Description("Destination table name."),
				),
				mcp.WithArray(
					"rows",
					mcp.Required(),
					mcp.Description("Array of JSON-serializable rows to store in the json column."),
				),
				mcp.WithBoolean(
					"replace",
					mcp.Description(
						"When true, clear the table before inserting rows. Defaults to true.",
					),
				),
			),
			svc.handleStageRows,
		},
	}

	for _, t := range tools {
		reg.RegisterWithSpec(t.spec, t.handler)
	}

	return nil
}

// ── Tool handlers ─────────────────────────────────────────────────────────────

func (s *service) handleQuery(ctx context.Context, args map[string]any) (any, error) {
	query, _ := args["sql"].(string)
	if query == "" {
		return nil, errors.New("sql argument is required")
	}

	rows, err := s.db.QueryContext(ctx, query, toSlice(args["params"])...)
	if err != nil {
		return nil, errors.Wrapf(err, "db.query")
	}
	defer rows.Close()

	results, err := scanRows(rows)
	if err != nil {
		return nil, errors.Wrap(err, "db.query")
	}
	return results, nil
}

func (s *service) handleExec(ctx context.Context, args map[string]any) (any, error) {
	query, _ := args["sql"].(string)
	if query == "" {
		return nil, errors.New("sql argument is required")
	}

	res, err := s.db.ExecContext(ctx, query, toSlice(args["params"])...)
	if err != nil {
		return nil, errors.Wrapf(err, "db.exec")
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return nil, errors.Wrap(err, "db.exec rows affected")
	}
	return map[string]any{"rows_affected": rowsAffected}, nil
}

func (s *service) handleVecSearch(ctx context.Context, args map[string]any) (any, error) {
	table, _ := args["table"].(string)
	if table == "" {
		return nil, errors.New("table argument is required")
	}

	vectorBytes, err := encodeVector(args["vector"])
	if err != nil {
		return nil, errors.Wrapf(err, "db.vec_search")
	}

	limit := 10
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}

	minScore, _ := args["min_score"].(float64)

	query := fmt.Sprintf(
		`SELECT rowid, distance FROM %s WHERE embedding MATCH ? AND k = ?`,
		table,
	)
	rows, err := s.db.QueryContext(ctx, query, vectorBytes, limit)
	if err != nil {
		return nil, errors.Wrapf(err, "db.vec_search")
	}
	defer rows.Close()

	type searchResult struct {
		RowID    int64   `json:"rowid"`
		Distance float64 `json:"distance"`
	}

	var results []searchResult
	for rows.Next() {
		var result searchResult
		if err := rows.Scan(&result.RowID, &result.Distance); err != nil {
			return nil, errors.Wrap(err, "db.vec_search scan")
		}
		if minScore > 0 && result.Distance > minScore {
			continue
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "db.vec_search rows")
	}
	return results, nil
}

func (s *service) handleStageRows(ctx context.Context, args map[string]any) (any, error) {
	table, _ := args["table"].(string)
	if table == "" {
		return nil, errors.New("table argument is required")
	}
	if !sqliteIdentifierPattern.MatchString(table) {
		return nil, errors.New("table must be a valid SQLite identifier")
	}

	rowsArg, hasRows := args["rows"]
	if !hasRows {
		return nil, errors.New("rows argument is required")
	}
	rawRows, err := toAnyItems(rowsArg)
	if err != nil {
		return nil, errors.Wrapf(err, "db.stage_rows")
	}

	replace := true
	if raw, ok := args["replace"].(bool); ok {
		replace = raw
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "db.stage_rows begin tx")
	}
	defer tx.Rollback() //nolint:errcheck

	createSQL := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (json TEXT NOT NULL)`, table)
	if _, err := tx.ExecContext(ctx, createSQL); err != nil {
		return nil, errors.Wrapf(err, "db.stage_rows create table")
	}
	if replace {
		deleteSQL := fmt.Sprintf(`DELETE FROM %s`, table)
		if _, err := tx.ExecContext(ctx, deleteSQL); err != nil {
			return nil, errors.Wrapf(err, "db.stage_rows delete")
		}
	}

	insertSQL := fmt.Sprintf(`INSERT INTO %s (json) VALUES (?)`, table)
	stmt, err := tx.PrepareContext(ctx, insertSQL)
	if err != nil {
		return nil, errors.Wrapf(err, "db.stage_rows prepare")
	}
	defer stmt.Close()

	for i, row := range rawRows {
		payload, err := json.Marshal(row)
		if err != nil {
			return nil, errors.Wrapf(err, "db.stage_rows marshal row %d", i)
		}
		if _, err := stmt.ExecContext(ctx, string(payload)); err != nil {
			return nil, errors.Wrapf(err, "db.stage_rows insert row %d", i)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrapf(err, "db.stage_rows commit")
	}

	return map[string]any{
		"table":         table,
		"rows_inserted": len(rawRows),
		"replaced":      replace,
	}, nil
}

// ── SQL helpers ───────────────────────────────────────────────────────────────

func scanRows(rows *sql.Rows) ([]map[string]any, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, errors.Wrap(err, "get columns")
	}

	results := make([]map[string]any, 0)
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

func toSlice(v any) []any {
	if v == nil {
		return nil
	}
	if s, ok := v.([]any); ok {
		return s
	}
	return []any{v}
}

func encodeVector(v any) ([]byte, error) {
	switch val := v.(type) {
	case []byte:
		return val, nil
	case string:
		return []byte(val), nil
	case []any:
		floats := make([]float32, len(val))
		for i, item := range val {
			num, ok := item.(float64)
			if !ok {
				return nil, errors.Newf("vector element %d has unsupported type %T", i, item)
			}
			floats[i] = float32(num)
		}
		return encodeVector(floats)
	case []float32:
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

func toAnyItems(v any) ([]any, error) {
	if v == nil {
		return []any{}, nil
	}
	if items, ok := v.([]any); ok {
		return items, nil
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, errors.Newf("expected rows to be a slice or array, got %T", v)
	}
	items := make([]any, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		items[i] = rv.Index(i).Interface()
	}
	return items, nil
}
