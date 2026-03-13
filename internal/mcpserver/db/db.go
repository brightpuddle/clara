package db

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/driver"
	"github.com/rs/zerolog"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"

	_ "github.com/asg017/sqlite-vec-go-bindings/ncruces"
	_ "github.com/ncruces/go-sqlite3/embed"
)

const Description = "Built-in SQLite MCP server with query, exec, and vector search tools."

func init() {
	features := api.CoreFeaturesV2 | experimental.CoreFeaturesThreads
	sqlite3.RuntimeConfig = wazero.NewRuntimeConfig().WithCoreFeatures(features)
}

type Service struct {
	db *sql.DB
}

func Open(path string, _ zerolog.Logger) (*Service, error) {
	resolvedPath := path
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

func (s *Service) Close() error {
	return s.db.Close()
}

func (s *Service) NewServer() *server.MCPServer {
	mcpServer := server.NewMCPServer("clara-db", "0.1.0", server.WithToolCapabilities(true))

	mcpServer.AddTool(mcp.NewTool("query",
		mcp.WithDescription("Execute a SQL query and return the results."),
		mcp.WithString("sql", mcp.Required(), mcp.Description("SQL query text to execute.")),
		mcp.WithArray("params", mcp.Description("Optional positional parameters bound to the SQL query.")),
	), s.handleQuery)

	mcpServer.AddTool(mcp.NewTool("exec",
		mcp.WithDescription("Execute a SQL statement and return the number of affected rows."),
		mcp.WithString("sql", mcp.Required(), mcp.Description("SQL statement text to execute.")),
		mcp.WithArray("params", mcp.Description("Optional positional parameters bound to the SQL statement.")),
	), s.handleExec)

	mcpServer.AddTool(mcp.NewTool("vec_search",
		mcp.WithDescription("Perform a vector similarity search over a sqlite-vec table."),
		mcp.WithString("table", mcp.Required(), mcp.Description("Name of the vec0 virtual table to query.")),
		mcp.WithArray("vector", mcp.Required(), mcp.Description("Query vector represented as a JSON array of floats or raw bytes.")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of matches to return. Defaults to 10.")),
		mcp.WithNumber("min_score", mcp.Description("Optional maximum distance threshold for returned matches.")),
	), s.handleVecSearch)

	return mcpServer
}

func (s *Service) handleQuery(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query, err := stringArg(req, "sql")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	rows, err := s.db.QueryContext(ctx, query, toSlice(req.GetArguments()["params"])...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("db.query: %v", err)), nil
	}
	defer rows.Close()

	result, err := scanRows(rows)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("db.query: %v", err)), nil
	}
	return mcp.NewToolResultStructuredOnly(result), nil
}

func (s *Service) handleExec(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query, err := stringArg(req, "sql")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	res, err := s.db.ExecContext(ctx, query, toSlice(req.GetArguments()["params"])...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("db.exec: %v", err)), nil
	}
	rowsAffected, _ := res.RowsAffected()
	return mcp.NewToolResultStructuredOnly(map[string]any{"rows_affected": rowsAffected}), nil
}

func (s *Service) handleVecSearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	table, err := stringArg(req, "table")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	vectorBytes, err := encodeVector(req.GetArguments()["vector"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("db.vec_search: %v", err)), nil
	}

	limit := 10
	if l, ok := req.GetArguments()["limit"].(float64); ok && l > 0 {
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

	minScore, _ := req.GetArguments()["min_score"].(float64)
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

func stringArg(req mcp.CallToolRequest, name string) (string, error) {
	value, ok := req.GetArguments()[name].(string)
	if !ok || value == "" {
		return "", errors.Newf("%s argument is required", name)
	}
	return value, nil
}

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
