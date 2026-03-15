package db

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/brightpuddle/clara/internal/interpreter"
	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"
)

func TestRemindersTaskwarriorSyncPoCIntent(t *testing.T) {
	intent := loadPoCIntent(t)

	svc, err := Open("", zerolog.Nop())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer svc.Close()

	if _, err := svc.db.ExecContext(context.Background(), `
		CREATE TABLE reminders_taskwarrior_map (
		  reminder_id TEXT PRIMARY KEY,
		  task_uuid TEXT NOT NULL UNIQUE
		);
		INSERT INTO reminders_taskwarrior_map (reminder_id, task_uuid) VALUES
		  ('r-tie', 't-tie'),
		  ('r-delete', 't-delete'),
		  ('r-task-newer', 't-task-newer');
	`); err != nil {
		t.Fatalf("seed mapping table: %v", err)
	}

	reminders := []map[string]any{
		{
			"identifier": "r-tie",
			"title":      "Reminder wins tie",
			"completed":  false,
			"list_name":  "Work",
			"created_at": "2026-03-13T09:00:00Z",
			"updated_at": "2026-03-14T10:00:00Z",
		},
		{
			"identifier": "r-new",
			"title":      "Reminder creates task",
			"completed":  true,
			"list_name":  "Home",
			"due_date":   "2026-03-20T00:00:00Z",
			"created_at": "2026-03-13T08:00:00Z",
			"updated_at": "2026-03-14T09:00:00Z",
		},
		{
			"identifier": "r-task-newer",
			"title":      "Old reminder title",
			"completed":  false,
			"list_name":  "Errands",
			"created_at": "2026-03-13T07:00:00Z",
			"updated_at": "2026-03-14T08:00:00Z",
		},
	}
	tasks := []map[string]any{
		{
			"uuid":        "t-tie",
			"description": "Task loses tie",
			"project":     "Work",
			"status":      "pending",
			"entry":       "2026-03-13T09:00:00Z",
			"modified":    "2026-03-14T10:00:00Z",
		},
		{
			"uuid":        "t-delete",
			"description": "Should be deleted",
			"project":     "Work",
			"status":      "pending",
			"entry":       "2026-03-13T09:00:00Z",
			"modified":    "2026-03-14T09:30:00Z",
		},
		{
			"uuid":        "t-no-project",
			"description": "Task creates reminder",
			"project":     "",
			"status":      "pending",
			"entry":       "2026-03-13T06:00:00Z",
			"modified":    "2026-03-14T11:00:00Z",
		},
		{
			"uuid":        "t-task-newer",
			"description": "Task updates reminder",
			"project":     "Errands",
			"status":      "pending",
			"entry":       "2026-03-13T07:00:00Z",
			"modified":    "2026-03-14T11:30:00Z",
		},
	}

	reg := registry.New(zerolog.Nop())
	it := interpreter.NewStarlark(reg, zerolog.Nop())

	registerDBTool := func(name string, fn func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
		reg.Register(name, func(ctx context.Context, args map[string]any) (any, error) {
			result, err := fn(
				ctx,
				mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}},
			)
			if err != nil {
				return nil, err
			}
			if result.IsError {
				return nil, structuredError(result)
			}
			return result.StructuredContent, nil
		})
	}
	registerDBTool("db.exec", svc.handleExec)
	registerDBTool("db.query", svc.handleQuery)
	registerDBTool("db.stage_rows", svc.handleStageRows)

	reg.Register(
		"mac.reminders_default_list",
		func(_ context.Context, _ map[string]any) (any, error) {
			return map[string]any{"identifier": "default-inbox", "list_name": "Inbox"}, nil
		},
	)
	reg.Register("mac.reminders_list", func(_ context.Context, _ map[string]any) (any, error) {
		return cloneRows(reminders), nil
	})
	reg.Register("mac.reminders_create", func(_ context.Context, args map[string]any) (any, error) {
		identifier := "r-created-" + string(rune('a'+len(reminders)))
		reminder := map[string]any{
			"identifier":      identifier,
			"title":           stringArgValue(args["title"]),
			"completed":       boolArgValue(args["completed"]),
			"list_name":       stringArgValue(args["list_name"]),
			"due_date":        stringArgValue(args["due_date"]),
			"completion_date": stringArgValue(args["completion_date"]),
			"created_at":      "2026-03-14T12:00:00Z",
			"updated_at":      "2026-03-14T12:00:00Z",
		}
		reminders = append(reminders, reminder)
		return reminder, nil
	})
	reg.Register("mac.reminders_update", func(_ context.Context, args map[string]any) (any, error) {
		identifier := stringArgValue(args["identifier"])
		idx := findByKey(reminders, "identifier", identifier)
		if idx < 0 {
			t.Fatalf("reminder %q not found for update", identifier)
		}
		updateRow(reminders[idx], args)
		reminders[idx]["updated_at"] = "2026-03-14T12:30:00Z"
		return reminders[idx], nil
	})
	reg.Register("mac.reminders_delete", func(_ context.Context, args map[string]any) (any, error) {
		identifier := stringArgValue(args["identifier"])
		idx := findByKey(reminders, "identifier", identifier)
		if idx >= 0 {
			reminders = append(reminders[:idx], reminders[idx+1:]...)
		}
		return map[string]any{"identifier": identifier, "deleted": true}, nil
	})

	reg.Register("taskwarrior.list_tasks", func(_ context.Context, _ map[string]any) (any, error) {
		return cloneRows(tasks), nil
	})
	reg.Register("taskwarrior.task_add", func(_ context.Context, args map[string]any) (any, error) {
		uuid := "t-created-" + string(rune('a'+len(tasks)))
		task := map[string]any{
			"uuid":        uuid,
			"description": stringArgValue(args["description"]),
			"project":     stringArgValue(args["project"]),
			"due":         stringArgValue(args["due"]),
			"status":      "pending",
			"entry":       "2026-03-14T12:00:00Z",
			"modified":    "2026-03-14T12:00:00Z",
		}
		tasks = append(tasks, task)
		return task, nil
	})
	reg.Register(
		"taskwarrior.task_update",
		func(_ context.Context, args map[string]any) (any, error) {
			uuid := stringArgValue(args["uuid"])
			idx := findByKey(tasks, "uuid", uuid)
			if idx < 0 {
				t.Fatalf("task %q not found for update", uuid)
			}
			updateRow(tasks[idx], args)
			if status := stringArgValue(args["status"]); status != "" {
				tasks[idx]["status"] = status
			}
			tasks[idx]["modified"] = "2026-03-14T12:30:00Z"
			return tasks[idx], nil
		},
	)
	reg.Register(
		"taskwarrior.task_delete",
		func(_ context.Context, args map[string]any) (any, error) {
			uuid := stringArgValue(args["uuid"])
			idx := findByKey(tasks, "uuid", uuid)
			if idx >= 0 {
				tasks = append(tasks[:idx], tasks[idx+1:]...)
			}
			return map[string]any{"uuid": uuid, "deleted": true}, nil
		},
	)

	if err := it.Execute(context.Background(), intent, "", interpreter.RunOptions{RunID: "poc-test"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if findByKey(tasks, "uuid", "t-delete") >= 0 {
		t.Fatal("expected deleted mapped task to be removed")
	}
	if task := requireRow(t, tasks, "uuid", "t-tie"); task["description"] != "Reminder wins tie" {
		t.Fatalf("expected reminder to win tie, got task %#v", task)
	}
	if task := requireRow(t, tasks, "description", "Reminder creates task"); task["status"] != "completed" {
		t.Fatalf("expected created task to be completed, got %#v", task)
	}
	if reminder := requireRow(t, reminders, "title", "Task creates reminder"); reminder["list_name"] != "Inbox" {
		t.Fatalf("expected projectless task to create Inbox reminder, got %#v", reminder)
	}
	if task := requireRow(t, tasks, "uuid", "t-no-project"); task["project"] != "Inbox" {
		t.Fatalf("expected projectless task to be normalized to Inbox, got %#v", task)
	}
	if reminder := requireRow(t, reminders, "identifier", "r-task-newer"); reminder["title"] != "Task updates reminder" {
		t.Fatalf("expected newer task to update reminder, got %#v", reminder)
	}

	rows, err := svc.db.QueryContext(
		context.Background(),
		`SELECT reminder_id, task_uuid FROM reminders_taskwarrior_map ORDER BY reminder_id`,
	)
	if err != nil {
		t.Fatalf("query mappings: %v", err)
	}
	defer rows.Close()

	var gotMappings [][2]string
	for rows.Next() {
		var reminderID, taskUUID string
		if err := rows.Scan(&reminderID, &taskUUID); err != nil {
			t.Fatalf("scan mapping: %v", err)
		}
		gotMappings = append(gotMappings, [2]string{reminderID, taskUUID})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}

	wantMappings := [][2]string{
		{"r-created-d", "t-no-project"},
		{"r-new", "t-created-d"},
		{"r-task-newer", "t-task-newer"},
		{"r-tie", "t-tie"},
	}
	if !slices.Equal(gotMappings, wantMappings) {
		t.Fatalf("mappings mismatch: got %#v want %#v", gotMappings, wantMappings)
	}
}

func loadPoCIntent(t *testing.T) *orchestrator.Intent {
	t.Helper()

	path, data := mustReadPoCIntentFixture(t)
	intent, err := orchestrator.LoadIntentFile(path, data)
	if err != nil {
		t.Fatalf("LoadIntentFile(%q): %v", path, err)
	}
	return intent
}

func mustReadPoCIntentFixture(t *testing.T) (string, []byte) {
	t.Helper()

	baseDir := filepath.Join("..", "..", "..", "tmp")
	for _, name := range []string{
		"reminders_taskwarrior_sync.star",
		"reminders-taskwarrior-sync.star",
	} {
		path := filepath.Join(baseDir, name)
		data, err := os.ReadFile(path)
		if err == nil {
			return path, data
		}
		if !os.IsNotExist(err) {
			t.Fatalf("ReadFile(%q): %v", path, err)
		}
	}

	t.Fatalf("no reminders-taskwarrior-sync .star intent fixture found in %q", baseDir)
	return "", nil
}

func structuredError(result *mcp.CallToolResult) error {
	if text := result.Content; len(text) > 0 {
		return registryError(text[0].(mcp.TextContent).Text)
	}
	return registryError("tool returned structured error")
}

type registryError string

func (e registryError) Error() string { return string(e) }

func cloneRows(rows []map[string]any) []map[string]any {
	cloned := make([]map[string]any, len(rows))
	for i, row := range rows {
		next := make(map[string]any, len(row))
		for k, v := range row {
			next[k] = v
		}
		cloned[i] = next
	}
	return cloned
}

func updateRow(row map[string]any, args map[string]any) {
	for key, value := range args {
		if key == "uuid" || key == "identifier" {
			continue
		}
		row[key] = value
	}
}

func findByKey(rows []map[string]any, key, value string) int {
	for i, row := range rows {
		if stringArgValue(row[key]) == value {
			return i
		}
	}
	return -1
}

func requireRow(t *testing.T, rows []map[string]any, key, value string) map[string]any {
	t.Helper()
	idx := findByKey(rows, key, value)
	if idx < 0 {
		t.Fatalf("row with %s=%q not found in %#v", key, value, rows)
	}
	return rows[idx]
}

func stringArgValue(v any) string {
	s, _ := v.(string)
	return s
}

func boolArgValue(v any) bool {
	b, _ := v.(bool)
	return b
}
