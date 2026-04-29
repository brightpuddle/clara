package main

import (
	"reflect"
	"testing"

	"github.com/brightpuddle/clara/internal/toolcatalog"
)

func TestParseToolCallArgs(t *testing.T) {
	args, err := parseToolCallArgs([]string{
		"path=.",
		"limit=10",
		"enabled=true",
	})
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	want := map[string]any{
		"path":    ".",
		"limit":   10.0,
		"enabled": true,
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("parse error: got %#+v want %#+v", args, want)
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
