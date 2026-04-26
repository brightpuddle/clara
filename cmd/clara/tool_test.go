package main

import (
	"reflect"
	"testing"

	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/toolcatalog"
)

func TestParseToolCallArgs(t *testing.T) {
	args, err := parseToolCallArgs([]string{
		"path=.",
		"limit=10",
		"enabled=true",
		`params=[1,"two"]`,
	})
	if err != nil {
		t.Fatalf("parseToolCallArgs returned error: %v", err)
	}

	if got, want := args["path"], "."; got != want {
		t.Fatalf("path: got %v want %v", got, want)
	}
	if got, want := args["limit"], float64(10); got != want {
		t.Fatalf("limit: got %v want %v", got, want)
	}
	if got, want := args["enabled"], true; got != want {
		t.Fatalf("enabled: got %v want %v", got, want)
	}
	if got, want := args["params"], []any{float64(1), "two"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("params: got %#v want %#v", got, want)
	}
}

func TestParseToolCallArgsRejectsDuplicateKeys(t *testing.T) {
	_, err := parseToolCallArgs([]string{"path=.", "path=.."})
	if err == nil {
		t.Fatal("expected duplicate-key error")
	}
}

func TestFormatToolList(t *testing.T) {
	tool := toolDetails{
		Name:        "db.query",
		Description: "Execute a SQL query and return the results.",
		Parameters: []toolParam{
			{Name: "sql", Type: "string", Required: true},
			{Name: "params", Type: "array", Required: false},
		},
	}

	got := toolcatalog.FormatToolList([]toolDetails{tool}, false)
	want := "db.query\n  Execute a SQL query and return the results.\n  sql: str\n  params?: list"
	if got != want {
		t.Fatalf("formatted list: got %q want %q", got, want)
	}
}

func TestFilterToolsByPrefix(t *testing.T) {
	tools := []registry.ToolInfo{{Name: "db.query"}, {Name: "db.exec"}, {Name: "shell.run"}}

	got := filterTools(tools, "db")
	if len(got) != 2 {
		t.Fatalf("expected 2 filtered tools, got %d", len(got))
	}
	if got[0].Name != "db.query" || got[1].Name != "db.exec" {
		t.Fatalf("unexpected filtered tools: %#v", got)
	}
}
