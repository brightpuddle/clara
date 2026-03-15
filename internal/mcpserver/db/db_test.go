package db

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"
)

func TestStageRowsStoresJSONRows(t *testing.T) {
	svc, err := Open("", zerolog.Nop())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer svc.Close()

	result, err := svc.handleStageRows(
		context.Background(),
		mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{
			"table": "reminders_stage",
			"rows": []any{
				map[string]any{"identifier": "r1", "title": "Buy milk"},
				map[string]any{"identifier": "r2", "title": "Pay rent"},
			},
		}}},
	)
	if err != nil {
		t.Fatalf("handleStageRows: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success result, got %#v", result.StructuredContent)
	}

	rows, err := svc.db.QueryContext(
		context.Background(),
		`SELECT json FROM reminders_stage ORDER BY rowid`,
	)
	if err != nil {
		t.Fatalf("QueryContext: %v", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 staged rows, got %d", count)
	}
}

func TestStageRowsRejectsInvalidTableName(t *testing.T) {
	svc, err := Open("", zerolog.Nop())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer svc.Close()

	result, err := svc.handleStageRows(
		context.Background(),
		mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{
			"table": "bad-name;",
			"rows":  []any{},
		}}},
	)
	if err != nil {
		t.Fatalf("handleStageRows: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected invalid table name to produce an MCP error result")
	}
}

func TestHandleQueryReturnsEmptyArrayForNoRows(t *testing.T) {
	svc, err := Open("", zerolog.Nop())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer svc.Close()

	if _, err := svc.db.ExecContext(context.Background(), `CREATE TABLE empty_test (id INTEGER)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	result, err := svc.handleQuery(
		context.Background(),
		mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{
			"sql": `SELECT id FROM empty_test`,
		}}},
	)
	if err != nil {
		t.Fatalf("handleQuery: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success result, got %#v", result.Content)
	}
	rows, ok := result.StructuredContent.([]map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %#v", result.StructuredContent)
	}
	if rows == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if len(rows) != 0 {
		t.Fatalf("expected no rows, got %#v", rows)
	}
}
