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

func TestTaskUpdateCompletesAfterApplyingFieldChanges(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "task.log")
	script := filepath.Join(dir, "task")
	if err := os.WriteFile(script, []byte(`#!/bin/sh
printf '%s\n' "$@" >> "`+logPath+`"
case "$*" in
  *"export")
    /bin/cat <<'JSON'
[
  {"uuid":"task-1","description":"old title","status":"pending","project":"Inbox","entry":"2026-03-12T00:00:00Z"}
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
	result, err := svc.handleTaskUpdate(
		context.Background(),
		mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{
			"uuid":        "task-1",
			"description": "new title",
			"status":      "completed",
		}}},
	)
	if err != nil {
		t.Fatalf("handleTaskUpdate returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success result, got %#v", result.Content)
	}

	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fake task log: %v", err)
	}
	logText := string(logged)
	if !strings.Contains(logText, "task-1\nmodify\ndescription:new title") {
		t.Fatalf("expected modify before complete, got %q", logText)
	}
	if !strings.Contains(logText, "task-1\ndone") {
		t.Fatalf("expected done command, got %q", logText)
	}
}

func TestExportTasksStripsConfigurationOverridePrefix(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "task")
	if err := os.WriteFile(script, []byte(`#!/bin/sh
case "$*" in
  *"export")
    /bin/cat <<'OUT'
Configuration override rc.json.array=on
Configuration override rc.confirmation=no
[
  {"uuid":"task-1","description":"ok","status":"pending","entry":"2026-03-12T00:00:00Z"}
]
OUT
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
	tasks, err := svc.exportTasks(context.Background(), nil)
	if err != nil {
		t.Fatalf("exportTasks returned error: %v", err)
	}
	if len(tasks) != 1 || tasks[0]["uuid"] != "task-1" {
		t.Fatalf("unexpected tasks: %#v", tasks)
	}
}

func TestExportTasksIncludesPayloadOnParseFailure(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "task")
	if err := os.WriteFile(script, []byte(`#!/bin/sh
case "$*" in
  *"export")
    /bin/cat <<'OUT'
Configuration override rc.json.array=on
[{"uuid":"task-1",}]
OUT
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
	_, err := svc.exportTasks(context.Background(), nil)
	if err == nil {
		t.Fatal("expected parse failure")
	}
	errText := err.Error()
	if !strings.Contains(errText, `[{\"uuid\":\"task-1\",}]`) {
		t.Fatalf("expected raw payload in error, got %q", errText)
	}
}
