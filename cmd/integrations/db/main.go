package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/brightpuddle/clara/pkg/contract"
	"github.com/hashicorp/go-plugin"
	"github.com/mark3labs/mcp-go/mcp"
)

type DBPlugin struct {
	service *Service
}

func (p *DBPlugin) Configure(config []byte) error {
	var cfg struct {
		Path string `json:"path"`
	}
	if len(config) > 0 {
		if err := json.Unmarshal(config, &cfg); err != nil {
			return err
		}
	}
	
	service, err := Open(cfg.Path)
	if err != nil {
		return err
	}
	p.service = service
	return nil
}

func (p *DBPlugin) Description() (string, error) {
	return Description, nil
}

func (p *DBPlugin) Tools() ([]byte, error) {
	tools := []mcp.Tool{
		mcp.NewTool(
			"query",
			mcp.WithDescription("Execute a SQL query and return the results."),
			mcp.WithString("sql", mcp.Required(), mcp.Description("SQL query text to execute.")),
			mcp.WithArray(
				"params",
				mcp.Description("Optional positional parameters bound to the SQL query."),
			),
		),
		mcp.NewTool(
			"exec",
			mcp.WithDescription("Execute a SQL statement and return the number of affected rows."),
			mcp.WithString("sql", mcp.Required(), mcp.Description("SQL statement text to execute.")),
			mcp.WithArray(
				"params",
				mcp.Description("Optional positional parameters bound to the SQL statement."),
			),
		),
		mcp.NewTool(
			"vec_search",
			mcp.WithDescription("Perform a vector similarity search over a sqlite-vec table."),
			mcp.WithString(
				"table",
				mcp.Required(),
				mcp.Description("Name of the vec0 virtual table to query."),
			),
			mcp.WithArray(
				"vector",
				mcp.Required(),
				mcp.Description("Query vector represented as a JSON array of floats or raw bytes."),
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
		mcp.NewTool(
			"stage_rows",
			mcp.WithDescription(
				"Stage structured rows into a SQLite table with a single json column for later SQL processing.",
			),
			mcp.WithString("table", mcp.Required(), mcp.Description("Destination table name.")),
			mcp.WithArray(
				"rows",
				mcp.Required(),
				mcp.Description("Array of JSON-serializable rows to store in the json column."),
			),
			mcp.WithBoolean(
				"replace",
				mcp.Description("When true, clear the table before inserting rows. Defaults to true."),
			),
		),
	}
	return json.Marshal(tools)
}

func (p *DBPlugin) CallTool(name string, args []byte) ([]byte, error) {
	if p.service == nil {
		return nil, fmt.Errorf("DBPlugin not configured")
	}

	var parsedArgs map[string]any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &parsedArgs); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = parsedArgs

	var res *mcp.CallToolResult
	var err error

	switch name {
	case "query":
		res, err = p.service.handleQuery(context.Background(), req)
	case "exec":
		res, err = p.service.handleExec(context.Background(), req)
	case "vec_search":
		res, err = p.service.handleVecSearch(context.Background(), req)
	case "stage_rows":
		res, err = p.service.handleStageRows(context.Background(), req)
	default:
		return nil, fmt.Errorf("tool %q not found in db plugin", name)
	}

	if err != nil {
		return nil, err
	}

	if res.IsError {
		var texts []string
		for _, c := range res.Content {
			if tc, ok := c.(mcp.TextContent); ok {
				texts = append(texts, tc.Text)
			}
		}
		return nil, fmt.Errorf("%s", strings.Join(texts, "\n"))
	}

	if res.StructuredContent != nil {
		return json.Marshal(res.StructuredContent)
	}

	if len(res.Content) == 1 {
		if tc, ok := res.Content[0].(mcp.TextContent); ok {
			return []byte(tc.Text), nil
		}
	}

	return json.Marshal(res.Content)
}

func (p *DBPlugin) Query(sql string, params []any) ([]map[string]any, error) {
	if p.service == nil {
		return nil, fmt.Errorf("DBPlugin not configured")
	}
	return p.service.Query(context.Background(), sql, params)
}

func (p *DBPlugin) Exec(sql string, params []any) (int64, error) {
	if p.service == nil {
		return 0, fmt.Errorf("DBPlugin not configured")
	}
	return p.service.Exec(context.Background(), sql, params)
}

func (p *DBPlugin) VecSearch(table string, vector []float32, limit int, minScore float64) ([]map[string]any, error) {
	if p.service == nil {
		return nil, fmt.Errorf("DBPlugin not configured")
	}

	// This is a bit redundant because handleVecSearch does its own thing,
	// but for the static Go interface we'll re-implement or call a common helper.
	// For now, let's just implement it here to satisfy the interface.
	vectorBytes, err := encodeVector(vector)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`SELECT rowid, distance FROM %s WHERE embedding MATCH ? AND k = ?`, table)
	rows, err := p.service.db.QueryContext(context.Background(), query, vectorBytes, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]any
	for rows.Next() {
		var rowid int64
		var distance float64
		if err := rows.Scan(&rowid, &distance); err != nil {
			return nil, err
		}
		if minScore > 0 && distance > minScore {
			continue
		}
		results = append(results, map[string]any{
			"rowid":    rowid,
			"distance": distance,
		})
	}
	return results, rows.Err()
}

func (p *DBPlugin) StageRows(table string, rows []any, replace bool) (int, error) {
	if p.service == nil {
		return 0, fmt.Errorf("DBPlugin not configured")
	}

	// Again, reusing logic from handleStageRows but for the Go interface.
	// We'll wrap it in an mcp.CallToolRequest to reuse the existing handler logic
	// or just call handleStageRows and extract the result.
	// Calling handleStageRows is easiest.

	req := mcp.CallToolRequest{}
	req.Params.Name = "stage_rows"
	req.Params.Arguments = map[string]any{
		"table":   table,
		"rows":    rows,
		"replace": replace,
	}

	res, err := p.service.handleStageRows(context.Background(), req)
	if err != nil {
		return 0, err
	}
	if res.IsError {
		return 0, fmt.Errorf("stage_rows failed")
	}

	if m, ok := res.StructuredContent.(map[string]any); ok {
		if inserted, ok := m["rows_inserted"].(int); ok {
			return inserted, nil
		}
	}

	return 0, nil
}

func main() {
	impl := &DBPlugin{}
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: contract.HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"db": &contract.DBIntegrationPlugin{Impl: impl},
		},
	})
}
