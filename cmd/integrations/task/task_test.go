package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brightpuddle/clara/pkg/contract"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// fakeTaskBinary writes a shell script to dir/task that echoes args to a log
// file and, when called with "export", prints the given JSON.
func fakeTaskBinary(t *testing.T, dir, exportJSON string) (binPath, logPath string) {
	t.Helper()
	logPath = filepath.Join(dir, "task.log")
	binPath = filepath.Join(dir, "task")
	script := `#!/bin/sh
printf '%s\n' "$@" >> "` + logPath + `"
case "$*" in
  *"export")
    /bin/cat <<'JSON'
` + exportJSON + `
JSON
    ;;
  *)
    /bin/echo '{}'
    ;;
esac
`
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake task binary: %v", err)
	}
	return binPath, logPath
}

func mustReadLog(t *testing.T, logPath string) string {
	t.Helper()
	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read task log: %v", err)
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// Description / Tools
// ---------------------------------------------------------------------------

func TestDescription(t *testing.T) {
	tw := &Task{taskPath: "/usr/bin/task"}
	desc, err := tw.Description()
	if err != nil {
		t.Fatalf("Description() error: %v", err)
	}
	if desc == "" {
		t.Fatal("Description() returned empty string")
	}
}

func TestTools_ValidJSON(t *testing.T) {
	tw := &Task{taskPath: "/usr/bin/task"}
	raw, err := tw.Tools()
	if err != nil {
		t.Fatalf("Tools() error: %v", err)
	}
	var tools []any
	if err := json.Unmarshal(raw, &tools); err != nil {
		t.Fatalf("Tools() returned invalid JSON: %v", err)
	}
	if len(tools) == 0 {
		t.Fatal("Tools() returned no tools")
	}
}

func TestTools_ExpectedNames(t *testing.T) {
	tw := &Task{taskPath: "/usr/bin/task"}
	raw, _ := tw.Tools()
	var tools []map[string]any
	_ = json.Unmarshal(raw, &tools)
	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		if n, ok := tool["name"].(string); ok {
			names[n] = true
		}
	}
	for _, want := range []string{
		"task.create", "task.get", "task.update", "task.delete",
		"task.list", "pending.list", "due.list",
	} {
		if !names[want] {
			t.Errorf("expected tool %q to be registered", want)
		}
	}
}

// ---------------------------------------------------------------------------
// parseTaskTime
// ---------------------------------------------------------------------------

func TestParseTaskTime(t *testing.T) {
	cases := []struct {
		input string
		want  string // expected UTC RFC3339
	}{
		{"2026-03-13T00:00:00Z", "2026-03-13T00:00:00Z"},
		{"20260313T000000Z", "2026-03-13T00:00:00Z"},
		{"2026-03-13", "2026-03-13T00:00:00Z"},
	}
	for _, tc := range cases {
		got, err := parseTaskTime(tc.input)
		if err != nil {
			t.Errorf("parseTaskTime(%q) error: %v", tc.input, err)
			continue
		}
		if got.UTC().Format(time.RFC3339) != tc.want {
			t.Errorf(
				"parseTaskTime(%q) = %q, want %q",
				tc.input,
				got.UTC().Format(time.RFC3339),
				tc.want,
			)
		}
	}
}

func TestParseTaskTime_InvalidFormat(t *testing.T) {
	_, err := parseTaskTime("not-a-timestamp")
	if err == nil {
		t.Fatal("expected error for invalid timestamp")
	}
}

// ---------------------------------------------------------------------------
// buildAddArgs
// ---------------------------------------------------------------------------

func TestBuildAddArgs_Empty(t *testing.T) {
	args := buildAddArgs(contract.AddTaskParams{Description: "test"})
	if len(args) != 0 {
		t.Errorf("expected no extra args for minimal params, got %v", args)
	}
}

func TestBuildAddArgs_AllFields(t *testing.T) {
	args := buildAddArgs(contract.AddTaskParams{
		Project:    "work",
		Tags:       []string{"urgent", "home"},
		Status:     "pending",
		Priority:   "H",
		Due:        "2026-03-13",
		Wait:       "2026-03-10",
		ReminderID: "rem-123",
	})
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"project:work", "status:pending", "priority:H",
		"due:2026-03-13", "wait:2026-03-10", "reminder_id:rem-123",
		"+urgent", "+home",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in args %v", want, args)
		}
	}
}

// ---------------------------------------------------------------------------
// buildModifyArgs
// ---------------------------------------------------------------------------

func TestBuildModifyArgs_SetDescription(t *testing.T) {
	args := buildModifyArgs(contract.UpdateTaskParams{
		UUID:        "abc",
		Description: "new title",
	}, taskRecord{})
	if !sliceContains(args, "description:new title") {
		t.Errorf("expected description token, got %v", args)
	}
}

func TestBuildModifyArgs_ClearFields(t *testing.T) {
	args := buildModifyArgs(contract.UpdateTaskParams{
		UUID:            "abc",
		ClearProject:    true,
		ClearPriority:   true,
		ClearDue:        true,
		ClearWait:       true,
		ClearReminderID: true,
	}, taskRecord{})
	for _, want := range []string{"project:", "priority:", "due:", "wait:", "reminder_id:"} {
		if !sliceContains(args, want) {
			t.Errorf("expected %q in args %v", want, args)
		}
	}
}

func TestBuildModifyArgs_TagDiff(t *testing.T) {
	current := taskRecord{"tags": []any{"home", "work"}}
	params := contract.UpdateTaskParams{
		UUID:    "abc",
		Tags:    []string{"home", "personal"},
		SetTags: true,
	}
	args := buildModifyArgs(params, current)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-work") {
		t.Errorf("expected -work in args %v", args)
	}
	if !strings.Contains(joined, "+personal") {
		t.Errorf("expected +personal in args %v", args)
	}
	if strings.Contains(joined, "-home") || strings.Contains(joined, "+home") {
		t.Errorf("home should not appear (unchanged): %v", args)
	}
}

func TestBuildModifyArgs_SkipsCompletedStatus(t *testing.T) {
	args := buildModifyArgs(contract.UpdateTaskParams{
		UUID:   "abc",
		Status: "completed",
	}, taskRecord{})
	for _, arg := range args {
		if strings.HasPrefix(arg, "status:") {
			t.Errorf("completed status should not appear in modify args, got %v", args)
		}
	}
}

// ---------------------------------------------------------------------------
// buildFilters
// ---------------------------------------------------------------------------

func TestBuildFilters(t *testing.T) {
	filters := buildFilters(contract.TaskFilter{
		Project:    "work",
		Tags:       []string{"urgent"},
		Status:     "pending",
		ReminderID: "rem-1",
	})
	joined := strings.Join(filters, " ")
	for _, want := range []string{"project:work", "+urgent", "status:pending", "reminder_id:rem-1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in filters %v", want, filters)
		}
	}
}

// ---------------------------------------------------------------------------
// extractJSONArray
// ---------------------------------------------------------------------------

func TestExtractJSONArray_StripsConfigPrefix(t *testing.T) {
	input := []byte(`Configuration override rc.json.array=on
Configuration override rc.confirmation=no
[{"uuid":"task-1"}]`)
	got := extractJSONArray(input)
	if string(got) != `[{"uuid":"task-1"}]` {
		t.Errorf("extractJSONArray = %q", got)
	}
}

func TestExtractJSONArray_NoPrefix(t *testing.T) {
	input := []byte(`[{"uuid":"task-1"}]`)
	got := extractJSONArray(input)
	if string(got) != `[{"uuid":"task-1"}]` {
		t.Errorf("extractJSONArray = %q", got)
	}
}

// ---------------------------------------------------------------------------
// findCreatedTask
// ---------------------------------------------------------------------------

func TestFindCreatedTask_FindsNew(t *testing.T) {
	before := []taskRecord{{"uuid": "old-1"}}
	after := []taskRecord{{"uuid": "old-1"}, {"uuid": "new-1"}}
	got, err := findCreatedTask(before, after)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["uuid"] != "new-1" {
		t.Errorf("expected new-1, got %v", got)
	}
}

func TestFindCreatedTask_NoneFound(t *testing.T) {
	before := []taskRecord{{"uuid": "a"}}
	after := []taskRecord{{"uuid": "a"}}
	_, err := findCreatedTask(before, after)
	if err == nil {
		t.Fatal("expected error when no new task found")
	}
}

// ---------------------------------------------------------------------------
// filterDueTasks
// ---------------------------------------------------------------------------

func TestFilterDueTasks(t *testing.T) {
	before := mustParseTime(t, "2026-03-14T00:00:00Z")
	tasks := []taskRecord{
		{"uuid": "due-1", "due": "2026-03-13T00:00:00Z"},
		{"uuid": "later-1", "due": "2026-03-15T00:00:00Z"},
		{"uuid": "no-due"},
	}
	got := filterDueTasks(tasks, before)
	if len(got) != 1 || got[0]["uuid"] != "due-1" {
		t.Errorf("filterDueTasks = %v, want only due-1", got)
	}
}

// ---------------------------------------------------------------------------
// filterUpdatedTasks
// ---------------------------------------------------------------------------

func TestFilterUpdatedTasks(t *testing.T) {
	after := mustParseTime(t, "2026-03-14T00:00:00Z")
	tasks := []taskRecord{
		{"uuid": "newer", "modified": "2026-03-15T00:00:00Z"},
		{"uuid": "older", "modified": "2026-03-13T00:00:00Z"},
		{"uuid": "no-mod"},
	}
	got := filterUpdatedTasks(tasks, after)
	if len(got) != 1 || got[0]["uuid"] != "newer" {
		t.Errorf("filterUpdatedTasks = %v, want only newer", got)
	}
}

// ---------------------------------------------------------------------------
// CallTool dispatch
// ---------------------------------------------------------------------------

func TestCallTool_UnknownTool(t *testing.T) {
	tw := &Task{taskPath: "/usr/bin/task"}
	_, err := tw.CallTool("does.not.exist", []byte("{}"))
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestCallTool_BadJSON(t *testing.T) {
	tw := &Task{taskPath: "/usr/bin/task"}
	for _, tool := range []string{
		"task.create", "task.get", "task.update", "task.delete",
		"task.list", "pending.list", "due.list",
	} {
		_, err := tw.CallTool(tool, []byte("not-json"))
		if err == nil {
			t.Errorf("CallTool(%q, bad-json): expected error, got nil", tool)
		}
	}
}

func TestCallTool_MissingRequiredUUID(t *testing.T) {
	tw := &Task{taskPath: "/usr/bin/task"}
	for _, tool := range []string{"task.get", "task.update", "task.delete"} {
		_, err := tw.CallTool(tool, []byte(`{}`))
		if err == nil {
			t.Errorf("CallTool(%q, missing uuid): expected error", tool)
		}
	}
}

func TestCallTool_MissingDescription(t *testing.T) {
	tw := &Task{taskPath: "/usr/bin/task"}
	_, err := tw.CallTool("task.create", []byte(`{}`))
	if err == nil {
		t.Fatal("task.create with no description should error")
	}
}

// ---------------------------------------------------------------------------
// unavailableStub
// ---------------------------------------------------------------------------

func TestUnavailableStub_AllMethodsReturnErr(t *testing.T) {
	sentinel := &sentinelErr{"unavailable"}
	s := &unavailableStub{err: sentinel}

	checks := []struct {
		name string
		fn   func() error
	}{
		{"Configure", func() error { return s.Configure(nil) }},
		{"Description", func() error { _, err := s.Description(); return err }},
		{"Tools", func() error { _, err := s.Tools(); return err }},
		{"CallTool", func() error { _, err := s.CallTool("", nil); return err }},
		{"AddTask", func() error { _, err := s.AddTask(contract.AddTaskParams{}); return err }},
		{"GetTask", func() error { _, err := s.GetTask(""); return err }},
		{"UpdateTask", func() error {
			_, err := s.UpdateTask(contract.UpdateTaskParams{})
			return err
		}},
		{"DeleteTask", func() error { return s.DeleteTask("") }},
		{"ListTasks", func() error { _, err := s.ListTasks(contract.TaskFilter{}); return err }},
		{
			"ListPending",
			func() error { _, err := s.ListPending(contract.TaskFilter{}); return err },
		},
		{"ListDue", func() error { _, err := s.ListDue(contract.DueFilter{}); return err }},
	}
	for _, c := range checks {
		if err := c.fn(); err != sentinel {
			t.Errorf("%s should return sentinel error, got %v", c.name, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Configure
// ---------------------------------------------------------------------------

func TestConfigure_AlwaysNil(t *testing.T) {
	tw := &Task{taskPath: "/usr/bin/task"}
	if err := tw.Configure(nil); err != nil {
		t.Errorf("Configure(nil) = %v, want nil", err)
	}
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

func TestTask_ImplementsInterface(t *testing.T) {
	var _ contract.TaskIntegration = (*Task)(nil)
	var _ contract.TaskIntegration = (*unavailableStub)(nil)
}

// ---------------------------------------------------------------------------
// Integration tests using fake `task` binary
// ---------------------------------------------------------------------------

const twoTasksJSON = `[
  {"uuid":"due-1","description":"due task","status":"pending","due":"2026-03-13T00:00:00Z","entry":"2026-03-12T00:00:00Z","tags":["home"]},
  {"uuid":"later-1","description":"later task","status":"pending","due":"2026-03-15T00:00:00Z","entry":"2026-03-12T00:00:00Z","tags":["home"]}
]`

func TestListDue_FiltersByBeforeTimestamp(t *testing.T) {
	dir := t.TempDir()
	fakeTaskBinary(t, dir, twoTasksJSON)
	t.Setenv("PATH", dir)

	tw, err := newTask()
	if err != nil {
		t.Fatalf("newTask: %v", err)
	}
	tasks, err := tw.ListDue(contract.DueFilter{
		Tags:   []string{"home"},
		Before: "2026-03-14T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("ListDue error: %v", err)
	}
	if len(tasks) != 1 || tasks[0].UUID != "due-1" {
		t.Errorf("expected only due-1, got %+v", tasks)
	}
}

func TestListDue_FiltersPassedToTask(t *testing.T) {
	dir := t.TempDir()
	_, logPath := fakeTaskBinary(t, dir, twoTasksJSON)
	t.Setenv("PATH", dir)

	tw, _ := newTask()
	_, _ = tw.ListDue(contract.DueFilter{
		Tags:   []string{"home"},
		Before: "2026-03-14T00:00:00Z",
	})

	log := mustReadLog(t, logPath)
	if !strings.Contains(log, "+home") || !strings.Contains(log, "status:pending") {
		t.Errorf("expected tag and status filters in task invocation, got:\n%s", log)
	}
}

func TestUpdateTask_ModifyThenDone(t *testing.T) {
	dir := t.TempDir()
	exportJSON := `[{"uuid":"task-1","description":"old","status":"pending","entry":"2026-03-12T00:00:00Z"}]`
	_, logPath := fakeTaskBinary(t, dir, exportJSON)
	t.Setenv("PATH", dir)

	tw, _ := newTask()
	_, err := tw.UpdateTask(contract.UpdateTaskParams{
		UUID:        "task-1",
		Description: "new title",
		Status:      "completed",
	})
	if err != nil {
		t.Fatalf("UpdateTask error: %v", err)
	}
	log := mustReadLog(t, logPath)
	if !strings.Contains(log, "task-1\nmodify\ndescription:new title") {
		t.Errorf("expected modify with description, got:\n%s", log)
	}
	if !strings.Contains(log, "task-1\ndone") {
		t.Errorf("expected done command, got:\n%s", log)
	}
}

func TestExportTasks_StripsConfigOverridePrefix(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
case "$*" in
  *"export")
    /bin/cat <<'OUT'
Configuration override rc.json.array=on
Configuration override rc.confirmation=no
[{"uuid":"task-1","description":"ok","status":"pending","entry":"2026-03-12T00:00:00Z"}]
OUT
    ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "task"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
	t.Setenv("PATH", dir)

	tw, _ := newTask()
	records, err := tw.exportTasks(context.Background(), nil)
	if err != nil {
		t.Fatalf("exportTasks error: %v", err)
	}
	if len(records) != 1 || records[0]["uuid"] != "task-1" {
		t.Fatalf("unexpected records: %#v", records)
	}
}

func TestExportTasks_ParseErrorIncludesPayload(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
/bin/cat <<'OUT'
[{"uuid":"task-1",}]
OUT
`
	if err := os.WriteFile(filepath.Join(dir, "task"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
	t.Setenv("PATH", dir)

	tw, _ := newTask()
	_, err := tw.exportTasks(context.Background(), nil)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "task-1") {
		t.Errorf("expected raw payload in error message, got: %v", err)
	}
}

func TestListTasks_UpdatedAfterFilter(t *testing.T) {
	dir := t.TempDir()
	exportJSON := `[
  {"uuid":"newer","description":"newer","status":"pending","entry":"2026-03-12T00:00:00Z","modified":"2026-03-15T00:00:00Z"},
  {"uuid":"older","description":"older","status":"pending","entry":"2026-03-12T00:00:00Z","modified":"2026-03-13T00:00:00Z"}
]`
	fakeTaskBinary(t, dir, exportJSON)
	t.Setenv("PATH", dir)

	tw, _ := newTask()
	tasks, err := tw.ListTasks(contract.TaskFilter{UpdatedAfter: "2026-03-14T00:00:00Z"})
	if err != nil {
		t.Fatalf("ListTasks error: %v", err)
	}
	if len(tasks) != 1 || tasks[0].UUID != "newer" {
		t.Errorf("expected only newer, got %+v", tasks)
	}
}

func TestUnavailableWhenBinaryMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := newTask()
	if err == nil {
		t.Fatal("expected error when task binary is missing")
	}
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func sliceContains(s []string, target string) bool {
	for _, v := range s {
		if v == target {
			return true
		}
	}
	return false
}

func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := parseTaskTime(s)
	if err != nil {
		t.Fatalf("mustParseTime(%q): %v", s, err)
	}
	return v
}

type sentinelErr struct{ msg string }

func (e *sentinelErr) Error() string { return e.msg }
