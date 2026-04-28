package main

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
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/driver"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"

	_ "github.com/asg017/sqlite-vec-go-bindings/ncruces"
	_ "github.com/ncruces/go-sqlite3/embed"
)

const Description = "Built-in SQLite integration with query, exec, and vector search tools."

func init() {
	features := api.CoreFeaturesV2 | experimental.CoreFeaturesThreads
	sqlite3.RuntimeConfig = wazero.NewRuntimeConfig().WithCoreFeatures(features)
}

type Service struct {
	db *sql.DB
}

var sqliteIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func Open(path string) (*Service, error) {
	resolvedPath := resolvePath(path)
	if resolvedPath == "" {
		resolvedPath = ":memory:"
	}
	if resolvedPath != ":memory:" {
		dir := filepath.Dir(resolvedPath)
		if dir != "." {
			if err := os.MkdirAll(dir, 0o750); err != nil {
				return nil, errors.Wrapf(err, "create db directory for %q", resolvedPath)
			}
		}
	}

	db, err := driver.Open(resolvedPath, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "open sqlite database %q", resolvedPath)
	}
	return &Service{db: db}, nil
}

func resolvePath(path string) string {
	path = os.ExpandEnv(strings.TrimSpace(path))
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func (s *Service) Close() error {
	return s.db.Close()
}

func (s *Service) Query(ctx context.Context, sql string, params []any) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, sql, params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanRows(rows)
}

func (s *Service) Exec(ctx context.Context, sql string, params []any) (int64, error) {
	res, err := s.db.ExecContext(ctx, sql, params...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Service) handleQuery(
	ctx context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args, _ := req.Params.Arguments.(map[string]any)
	query, _ := args["sql"].(string)
	if query == "" {
		return mcp.NewToolResultError("sql argument is required"), nil
	}

	results, err := s.Query(ctx, query, toSlice(args["params"]))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("db.query: %v", err)), nil
	}
	return mcp.NewToolResultStructuredOnly(results), nil
}

func (s *Service) handleExec(
	ctx context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args, _ := req.Params.Arguments.(map[string]any)
	query, _ := args["sql"].(string)
	if query == "" {
		return mcp.NewToolResultError("sql argument is required"), nil
	}

	rowsAffected, err := s.Exec(ctx, query, toSlice(args["params"]))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("db.exec: %v", err)), nil
	}
	return mcp.NewToolResultStructuredOnly(map[string]any{"rows_affected": rowsAffected}), nil
}

func (s *Service) handleVecSearch(
	ctx context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args, _ := req.Params.Arguments.(map[string]any)
	table, _ := args["table"].(string)
	if table == "" {
		return mcp.NewToolResultError("table argument is required"), nil
	}

	vectorBytes, err := encodeVector(args["vector"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("db.vec_search: %v", err)), nil
	}

	limit := 10
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}

	query := fmt.Sprintf(`SELECT rowid, distance FROM %s WHERE embedding MATCH ? AND k = ?`, table)
	rows, err := s.db.QueryContext(ctx, query, vectorBytes, limit)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("db.vec_search: %v", err)), nil
	}
	defer rows.Close()

	type searchResult struct {
		RowID    int64   `json:"rowid"`
		Distance float64 `json:"distance"`
	}

	minScore, _ := args["min_score"].(float64)
	var results []searchResult
	for rows.Next() {
		var result searchResult
		if err := rows.Scan(&result.RowID, &result.Distance); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("db.vec_search: %v", err)), nil
		}
		if minScore > 0 && result.Distance > minScore {
			continue
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("db.vec_search: %v", err)), nil
	}

	return mcp.NewToolResultStructuredOnly(results), nil
}

func (s *Service) handleStageRows(
	ctx context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args, _ := req.Params.Arguments.(map[string]any)
	table, _ := args["table"].(string)
	if table == "" {
		return mcp.NewToolResultError("table argument is required"), nil
	}
	if !sqliteIdentifierPattern.MatchString(table) {
		return mcp.NewToolResultError("table must be a valid SQLite identifier"), nil
	}

	rowsArg, hasRows := args["rows"]
	if !hasRows {
		return mcp.NewToolResultError("rows argument is required"), nil
	}
	rawRows, err := toAnyItems(rowsArg)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("db.stage_rows: %v", err)), nil
	}

	replace := true
	if raw, ok := args["replace"].(bool); ok {
		replace = raw
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("db.stage_rows: %v", err)), nil
	}
	defer tx.Rollback()

	createSQL := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (json TEXT NOT NULL)`, table)
	if _, err := tx.ExecContext(ctx, createSQL); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("db.stage_rows: %v", err)), nil
	}
	if replace {
		deleteSQL := fmt.Sprintf(`DELETE FROM %s`, table)
		if _, err := tx.ExecContext(ctx, deleteSQL); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("db.stage_rows: %v", err)), nil
		}
	}

	insertSQL := fmt.Sprintf(`INSERT INTO %s (json) VALUES (?)`, table)
	stmt, err := tx.PrepareContext(ctx, insertSQL)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("db.stage_rows: %v", err)), nil
	}
	defer stmt.Close()

	for i, row := range rawRows {
		payload, err := json.Marshal(row)
		if err != nil {
			return mcp.NewToolResultError(
				fmt.Sprintf("db.stage_rows: marshal row %d: %v", i, err),
			), nil
		}
		if _, err := stmt.ExecContext(ctx, string(payload)); err != nil {
			return mcp.NewToolResultError(
				fmt.Sprintf("db.stage_rows: insert row %d: %v", i, err),
			), nil
		}
	}
	if err := tx.Commit(); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("db.stage_rows: %v", err)), nil
	}

	return mcp.NewToolResultStructuredOnly(map[string]any{
		"table":         table,
		"rows_inserted": len(rawRows),
		"replaced":      replace,
	}), nil
}

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
