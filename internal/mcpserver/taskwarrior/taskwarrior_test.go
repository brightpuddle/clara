package taskwarrior

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"
)

func TestServiceUnavailableWhenTaskMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	svc := New(zerolog.Nop())
	if svc.availabilityErr == nil {
		t.Fatal("expected availability error when task binary is missing")
	}
	result, err := svc.handleListPending(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handleListPending returned error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected MCP error result when task is unavailable")
	}
}

func TestListDueFiltersExportedTasks(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "task.log")
	script := filepath.Join(dir, "task")
	if err := os.WriteFile(script, []byte(`#!/bin/sh
printf '%s\n' "$@" >> "`+logPath+`"
case "$*" in
  *"export")
    /bin/cat <<'JSON'
[
  {"uuid":"due-1","description":"due task","status":"pending","due":"2026-03-13T00:00:00Z","entry":"2026-03-12T00:00:00Z","tags":["home"]},
  {"uuid":"later-1","description":"later task","status":"pending","due":"2026-03-15T00:00:00Z","entry":"2026-03-12T00:00:00Z","tags":["home"]}
]
JSON
    ;;
  *)
    /bin/echo '{}'
    ;;
esac
`), 0o755); err != nil {
		t.Fatalf("write fake task binary: %v", err)
	}
	t.Setenv("PATH", dir)

	svc := New(zerolog.Nop())
	result, err := svc.handleListDue(
		context.Background(),
		mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{
			"tags":   []any{"home"},
			"before": "2026-03-14T00:00:00Z",
		}}},
	)
	if err != nil {
		t.Fatalf("handleListDue returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success result, got error content: %#v", result.Content)
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured result: %v", err)
	}
	if !strings.Contains(string(encoded), "due-1") || strings.Contains(string(encoded), "later-1") {
		t.Fatalf("unexpected due filter result: %s", encoded)
	}
	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fake task log: %v", err)
	}
	if !strings.Contains(string(logged), "+home\nstatus:pending\nexport") {
		t.Fatalf("expected task export filters in command log, got %q", string(logged))
	}
}
