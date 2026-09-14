package typestub_test

import (
	"strings"
	"testing"

	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/typestub"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"
)

func TestGenerateTypeScript(t *testing.T) {
	reg := registry.New(zerolog.Nop())

	reg.RegisterWithSpec(
		mcp.NewTool(
			"db.query",
			mcp.WithDescription("Execute SQL query"),
			mcp.WithString("sql", mcp.Required(), mcp.Description("SQL query statement")),
			mcp.WithNumber("limit", mcp.Description("Max rows")),
		),
		nil,
	)

	reg.RegisterWithSpec(
		mcp.NewTool(
			"fs.read_file",
			mcp.WithDescription("Read a file from disk"),
			mcp.WithString("path", mcp.Required(), mcp.Description("File path")),
		),
		nil,
	)

	ts := typestub.GenerateTypeScript(reg)

	if !strings.Contains(ts, "export interface CloudEvent<T = any>") {
		t.Errorf("expected CloudEvent definition in generated TypeScript")
	}

	if !strings.Contains(ts, "export interface ClaraTools") {
		t.Errorf("expected ClaraTools definition in generated TypeScript")
	}

	if !strings.Contains(ts, `"db.query": {`) {
		t.Errorf("expected db.query in ClaraTools: got:\n%s", ts)
	}

	if !strings.Contains(ts, "sql: string;") {
		t.Errorf("expected required sql string argument in db.query: got:\n%s", ts)
	}

	if !strings.Contains(ts, "limit?: number;") {
		t.Errorf("expected optional limit number argument in db.query: got:\n%s", ts)
	}

	if !strings.Contains(ts, `"fs.read_file": {`) {
		t.Errorf("expected fs.read_file in ClaraTools: got:\n%s", ts)
	}

	if !strings.Contains(ts, "export declare function callTool<K extends ClaraToolName>") {
		t.Errorf("expected callTool signature in generated TypeScript")
	}
}
