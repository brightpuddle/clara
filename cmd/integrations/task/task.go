package main

import (
	"context"
	"encoding/json"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/brightpuddle/clara/pkg/contract"
	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/mcp"
)

const description = "Task integration: manage tasks with CRUD, filtering, and due-task helpers."

// taskRecord is the raw JSON map returned by `task export`.
type taskRecord map[string]any

// Task implements contract.TaskIntegration.
type Task struct {
	taskPath string
}

func newTask() (*Task, error) {
	path, err := exec.LookPath("task")
	if err != nil {
		return nil, errors.Wrap(err, "task binary not found on PATH")
	}
	return &Task{taskPath: path}, nil
}

func (t *Task) Configure(_ []byte) error { return nil }

func (t *Task) Description() (string, error) { return description, nil }

func (t *Task) Tools() ([]byte, error) {
	tools := []mcp.Tool{
		mcp.NewTool(
			"create",
			mcp.WithDescription("Create a Task task and return the created task."),
			mcp.WithString("description", mcp.Required(), mcp.Description("Task description.")),
			mcp.WithString("project", mcp.Description("Optional project name.")),
			mcp.WithArray("tags", mcp.Description("Optional list of tags to assign.")),
			mcp.WithString(
				"status",
				mcp.Description("Optional initial status (pending or waiting)."),
			),
			mcp.WithString("priority", mcp.Description("Optional priority: H, M, or L.")),
			mcp.WithString(
				"due",
				mcp.Description("Optional due timestamp in Task or ISO-8601 format."),
			),
			mcp.WithString(
				"wait",
				mcp.Description("Optional wait timestamp in Task or ISO-8601 format."),
			),
			mcp.WithString(
				"reminder_id",
				mcp.Description("Optional Reminders/EventKit identifier to associate (UDA)."),
			),
		),
		mcp.NewTool(
			"get",
			mcp.WithDescription("Fetch a single task by UUID."),
			mcp.WithString("uuid", mcp.Required(), mcp.Description("Task UUID.")),
		),
		mcp.NewTool(
			"update",
			mcp.WithDescription("Update fields on an existing task and return the updated task."),
			mcp.WithString("uuid", mcp.Required(), mcp.Description("Task UUID.")),
			mcp.WithString("description", mcp.Description("Updated task description.")),
			mcp.WithString(
				"project",
				mcp.Description("Updated project name. Empty string clears the project."),
			),
			mcp.WithArray("tags", mcp.Description("Replacement set of task tags.")),
			mcp.WithString(
				"status",
				mcp.Description("Updated status: pending, waiting, or completed."),
			),
			mcp.WithString(
				"priority",
				mcp.Description("Updated priority. Empty string clears priority."),
			),
			mcp.WithString(
				"due",
				mcp.Description("Updated due timestamp. Empty string clears due."),
			),
			mcp.WithString(
				"wait",
				mcp.Description("Updated wait timestamp. Empty string clears wait."),
			),
			mcp.WithString(
				"reminder_id",
				mcp.Description("Updated Reminders/EventKit identifier. Empty string clears."),
			),
		),
		mcp.NewTool(
			"delete",
			mcp.WithDescription("Delete a task by UUID."),
			mcp.WithString("uuid", mcp.Required(), mcp.Description("Task UUID.")),
		),
		mcp.NewTool(
			"list",
			mcp.WithDescription("List tasks filtered by project, tags, status, and time."),
			mcp.WithString("project", mcp.Description("Optional project name filter.")),
			mcp.WithArray("tags", mcp.Description("Optional list of required tags.")),
			mcp.WithString("status", mcp.Description("Optional status filter.")),
			mcp.WithString(
				"updated_after",
				mcp.Description("Only return tasks modified on or after this timestamp."),
			),
			mcp.WithString(
				"reminder_id",
				mcp.Description("Only return the task matching this reminder_id UDA."),
			),
		),
		mcp.NewTool(
			"pending.list",
			mcp.WithDescription("List pending tasks, optionally filtered by project and tags."),
			mcp.WithString("project", mcp.Description("Optional project name filter.")),
			mcp.WithArray("tags", mcp.Description("Optional list of required tags.")),
		),
		mcp.NewTool(
			"due.list",
			mcp.WithDescription(
				"List pending tasks whose due date is on or before the given timestamp.",
			),
			mcp.WithString("project", mcp.Description("Optional project name filter.")),
			mcp.WithArray("tags", mcp.Description("Optional list of required tags.")),
			mcp.WithString(
				"before",
				mcp.Description("Upper-bound due timestamp; defaults to now."),
			),
		),
	}
	return json.Marshal(tools)
}

// --- CallTool args structs ---

type taskCreateArgs struct {
	Description string   `json:"description"`
	Project     string   `json:"project"`
	Tags        []string `json:"tags"`
	Status      string   `json:"status"`
	Priority    string   `json:"priority"`
	Due         string   `json:"due"`
	Wait        string   `json:"wait"`
	ReminderID  string   `json:"reminder_id"`
}

type taskUUIDArgs struct {
	UUID string `json:"uuid"`
}

type taskUpdateArgs struct {
	UUID        string   `json:"uuid"`
	Description string   `json:"description"`
	Project     string   `json:"project"`
	Tags        []string `json:"tags"`
	// tagsPresent is set by inspecting the raw JSON keys before unmarshalling.
	tagsPresent bool
	Status      string `json:"status"`
	Priority    string `json:"priority"`
	Due         string `json:"due"`
	Wait        string `json:"wait"`
	ReminderID  string `json:"reminder_id"`
}

type taskListArgs struct {
	Project      string   `json:"project"`
	Tags         []string `json:"tags"`
	Status       string   `json:"status"`
	UpdatedAfter string   `json:"updated_after"`
	ReminderID   string   `json:"reminder_id"`
}

type dueListArgs struct {
	Project string   `json:"project"`
	Tags    []string `json:"tags"`
	Before  string   `json:"before"`
}

func (t *Task) CallTool(name string, args []byte) ([]byte, error) {
	switch name {
	case "task.create":
		var a taskCreateArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, errors.Wrap(err, "unmarshal task.create args")
		}
		if a.Description == "" {
			return nil, errors.New("description is required")
		}
		task, err := t.AddTask(contract.AddTaskParams{
			Description: a.Description,
			Project:     a.Project,
			Tags:        a.Tags,
			Status:      a.Status,
			Priority:    a.Priority,
			Due:         a.Due,
			Wait:        a.Wait,
			ReminderID:  a.ReminderID,
		})
		if err != nil {
			return nil, err
		}
		return json.Marshal(task)

	case "task.get":
		var a taskUUIDArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, errors.Wrap(err, "unmarshal task.get args")
		}
		if a.UUID == "" {
			return nil, errors.New("uuid is required")
		}
		task, err := t.GetTask(a.UUID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(task)

	case "task.update":
		var a taskUpdateArgs
		// Detect whether "tags" key was present in the JSON payload before
		// unmarshalling into the struct (where absent and [] are both nil).
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(args, &raw); err != nil {
			return nil, errors.Wrap(err, "unmarshal task.update args")
		}
		_, a.tagsPresent = raw["tags"]
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, errors.Wrap(err, "unmarshal task.update args")
		}
		if a.UUID == "" {
			return nil, errors.New("uuid is required")
		}
		params := contract.UpdateTaskParams{
			UUID:        a.UUID,
			Description: a.Description,
			Project:     a.Project,
			Status:      a.Status,
			Priority:    a.Priority,
			Due:         a.Due,
			Wait:        a.Wait,
			ReminderID:  a.ReminderID,
			SetTags:     a.tagsPresent,
			Tags:        a.Tags,
		}
		task, err := t.UpdateTask(params)
		if err != nil {
			return nil, err
		}
		return json.Marshal(task)

	case "task.delete":
		var a taskUUIDArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, errors.Wrap(err, "unmarshal task.delete args")
		}
		if a.UUID == "" {
			return nil, errors.New("uuid is required")
		}
		if err := t.DeleteTask(a.UUID); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"uuid": a.UUID, "deleted": true})

	case "task.list":
		var a taskListArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, errors.Wrap(err, "unmarshal task.list args")
		}
		tasks, err := t.ListTasks(contract.TaskFilter{
			Project:      a.Project,
			Tags:         a.Tags,
			Status:       a.Status,
			UpdatedAfter: a.UpdatedAfter,
			ReminderID:   a.ReminderID,
		})
		if err != nil {
			return nil, err
		}
		return json.Marshal(tasks)

	case "pending.list":
		var a taskListArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, errors.Wrap(err, "unmarshal pending.list args")
		}
		tasks, err := t.ListPending(contract.TaskFilter{
			Project: a.Project,
			Tags:    a.Tags,
		})
		if err != nil {
			return nil, err
		}
		return json.Marshal(tasks)

	case "due.list":
		var a dueListArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, errors.Wrap(err, "unmarshal due.list args")
		}
		tasks, err := t.ListDue(contract.DueFilter{
			Project: a.Project,
			Tags:    a.Tags,
			Before:  a.Before,
		})
		if err != nil {
			return nil, err
		}
		return json.Marshal(tasks)

	default:
		return nil, errors.Newf("unknown tool: %q", name)
	}
}

// --- Typed interface methods ---

func (t *Task) AddTask(params contract.AddTaskParams) (contract.Task, error) {
	ctx := context.Background()

	before, err := t.exportTasks(ctx, nil)
	if err != nil {
		return contract.Task{}, err
	}

	cmdArgs := []string{"add", params.Description}
	cmdArgs = append(cmdArgs, buildAddArgs(params)...)
	if _, err := t.run(ctx, cmdArgs...); err != nil {
		return contract.Task{}, err
	}

	after, err := t.exportTasks(ctx, nil)
	if err != nil {
		return contract.Task{}, err
	}
	created, err := findCreatedTask(before, after)
	if err != nil {
		return contract.Task{}, err
	}
	return taskRecordToTask(created), nil
}

func (t *Task) GetTask(uuid string) (contract.Task, error) {
	rec, err := t.getRecord(context.Background(), uuid)
	if err != nil {
		return contract.Task{}, err
	}
	return taskRecordToTask(rec), nil
}

func (t *Task) UpdateTask(params contract.UpdateTaskParams) (contract.Task, error) {
	ctx := context.Background()
	current, err := t.getRecord(ctx, params.UUID)
	if err != nil {
		return contract.Task{}, err
	}

	modifyArgs := append([]string{params.UUID, "modify"}, buildModifyArgs(params, current)...)
	completingNow := params.Status == "completed" &&
		stringValue(current["status"]) != "completed"

	if len(modifyArgs) > 2 {
		if _, err := t.run(ctx, modifyArgs...); err != nil {
			return contract.Task{}, err
		}
	}
	if completingNow {
		if _, err := t.run(ctx, params.UUID, "done"); err != nil {
			return contract.Task{}, err
		}
	}
	if len(modifyArgs) == 2 && !completingNow {
		return contract.Task{}, errors.New("update requires at least one field to change")
	}

	rec, err := t.getRecord(ctx, params.UUID)
	if err != nil {
		return contract.Task{}, err
	}
	return taskRecordToTask(rec), nil
}

func (t *Task) DeleteTask(uuid string) error {
	_, err := t.run(context.Background(), uuid, "delete")
	return err
}

func (t *Task) ListTasks(filter contract.TaskFilter) ([]contract.Task, error) {
	tasks, err := t.exportTasks(context.Background(), buildFilters(filter))
	if err != nil {
		return nil, err
	}
	if filter.UpdatedAfter != "" {
		cutoff, err := parseTaskTime(filter.UpdatedAfter)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid updated_after timestamp")
		}
		tasks = filterUpdatedTasks(tasks, cutoff)
	}
	return taskRecordsToTasks(tasks), nil
}

func (t *Task) ListPending(filter contract.TaskFilter) ([]contract.Task, error) {
	filter.Status = "pending"
	return t.ListTasks(filter)
}

func (t *Task) ListDue(filter contract.DueFilter) ([]contract.Task, error) {
	taskFilter := contract.TaskFilter{
		Project: filter.Project,
		Tags:    filter.Tags,
		Status:  "pending",
	}
	tasks, err := t.exportTasks(context.Background(), buildFilters(taskFilter))
	if err != nil {
		return nil, err
	}

	before := time.Now().UTC()
	if filter.Before != "" {
		before, err = parseTaskTime(filter.Before)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid before timestamp")
		}
	}
	return taskRecordsToTasks(filterDueTasks(tasks, before)), nil
}

// --- Internal helpers ---

func (t *Task) getRecord(ctx context.Context, uuid string) (taskRecord, error) {
	records, err := t.exportTasks(ctx, []string{uuid})
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, errors.Newf("task %q not found", uuid)
	}
	return records[0], nil
}

func (t *Task) exportTasks(ctx context.Context, filters []string) ([]taskRecord, error) {
	args := append(append([]string{}, filters...), "export")
	output, err := t.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(string(output))
	if raw == "" {
		return nil, nil
	}
	payload := extractJSONArray(output)
	var records []taskRecord
	if err := json.Unmarshal(payload, &records); err != nil {
		return nil, errors.Wrapf(err, "parse task export json from payload %q", string(output))
	}
	for i := range records {
		normalizeRecord(records[i])
	}
	sort.Slice(records, func(i, j int) bool {
		return entryTime(records[i]).After(entryTime(records[j]))
	})
	return records, nil
}

func (t *Task) run(ctx context.Context, args ...string) ([]byte, error) {
	base := []string{"rc.json.array=on", "rc.confirmation=no"}
	cmd := exec.CommandContext(ctx, t.taskPath, append(base, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, errors.Wrapf(
			err,
			"task %s: %s",
			strings.Join(args, " "),
			strings.TrimSpace(string(out)),
		)
	}
	return out, nil
}

// buildAddArgs converts AddTaskParams into `task add` modifier tokens.
func buildAddArgs(p contract.AddTaskParams) []string {
	var args []string
	if p.Project != "" {
		args = append(args, "project:"+p.Project)
	}
	if p.Status != "" {
		args = append(args, "status:"+p.Status)
	}
	if p.Priority != "" {
		args = append(args, "priority:"+p.Priority)
	}
	if p.Due != "" {
		args = append(args, "due:"+p.Due)
	}
	if p.Wait != "" {
		args = append(args, "wait:"+p.Wait)
	}
	if p.ReminderID != "" {
		args = append(args, "reminder_id:"+p.ReminderID)
	}
	for _, tag := range p.Tags {
		if tag != "" {
			args = append(args, "+"+tag)
		}
	}
	return args
}

// buildModifyArgs converts UpdateTaskParams into `task modify` tokens.
// current is the existing task record (used for tag diffing).
func buildModifyArgs(p contract.UpdateTaskParams, current taskRecord) []string {
	var args []string
	if p.Description != "" {
		args = append(args, "description:"+p.Description)
	}
	switch {
	case p.ClearProject:
		args = append(args, "project:")
	case p.Project != "":
		args = append(args, "project:"+p.Project)
	}
	switch {
	case p.ClearPriority:
		args = append(args, "priority:")
	case p.Priority != "":
		args = append(args, "priority:"+p.Priority)
	}
	switch {
	case p.ClearDue:
		args = append(args, "due:")
	case p.Due != "":
		args = append(args, "due:"+p.Due)
	}
	switch {
	case p.ClearWait:
		args = append(args, "wait:")
	case p.Wait != "":
		args = append(args, "wait:"+p.Wait)
	}
	switch {
	case p.ClearReminderID:
		args = append(args, "reminder_id:")
	case p.ReminderID != "":
		args = append(args, "reminder_id:"+p.ReminderID)
	}
	// status:completed is handled separately via `task done`; skip it here.
	if p.Status != "" && p.Status != "completed" {
		args = append(args, "status:"+p.Status)
	}
	if p.SetTags {
		currentTags := normalizeStringSlice(current["tags"])
		newTags := normalizeStringSlice(p.Tags)
		for _, tag := range currentTags {
			if !slices.Contains(newTags, tag) {
				args = append(args, "-"+tag)
			}
		}
		for _, tag := range newTags {
			if !slices.Contains(currentTags, tag) {
				args = append(args, "+"+tag)
			}
		}
	}
	return args
}

// buildFilters converts a TaskFilter into `task export` filter tokens.
func buildFilters(f contract.TaskFilter) []string {
	var filters []string
	if f.Project != "" {
		filters = append(filters, "project:"+f.Project)
	}
	for _, tag := range normalizeStringSlice(f.Tags) {
		filters = append(filters, "+"+tag)
	}
	if f.Status != "" {
		filters = append(filters, "status:"+f.Status)
	}
	if f.ReminderID != "" {
		filters = append(filters, "reminder_id:"+f.ReminderID)
	}
	return filters
}

// extractJSONArray strips any leading "Configuration override ..." lines that
// `task` sometimes emits before the JSON array.
func extractJSONArray(output []byte) []byte {
	text := string(output)
	start := strings.IndexByte(text, '[')
	end := strings.LastIndexByte(text, ']')
	if start >= 0 && end >= start {
		return []byte(text[start : end+1])
	}
	return output
}

func findCreatedTask(before, after []taskRecord) (taskRecord, error) {
	seen := make(map[string]struct{}, len(before))
	for _, r := range before {
		if uuid, _ := r["uuid"].(string); uuid != "" {
			seen[uuid] = struct{}{}
		}
	}
	for _, r := range after {
		uuid, _ := r["uuid"].(string)
		if uuid == "" {
			continue
		}
		if _, ok := seen[uuid]; !ok {
			return r, nil
		}
	}
	return nil, errors.New("unable to identify the created task from export output")
}

func filterDueTasks(tasks []taskRecord, before time.Time) []taskRecord {
	out := make([]taskRecord, 0, len(tasks))
	for _, task := range tasks {
		raw, ok := task["due"].(string)
		if !ok || strings.TrimSpace(raw) == "" {
			continue
		}
		dueTime, err := parseTaskTime(raw)
		if err != nil {
			continue
		}
		if !dueTime.After(before) {
			out = append(out, task)
		}
	}
	return out
}

func filterUpdatedTasks(tasks []taskRecord, after time.Time) []taskRecord {
	out := make([]taskRecord, 0, len(tasks))
	for _, task := range tasks {
		raw, ok := task["modified"].(string)
		if !ok || strings.TrimSpace(raw) == "" {
			continue
		}
		mod, err := parseTaskTime(raw)
		if err != nil {
			continue
		}
		if !mod.Before(after) {
			out = append(out, task)
		}
	}
	return out
}

// parseTaskTime parses timestamps in the formats Task can emit.
func parseTaskTime(raw string) (time.Time, error) {
	for _, layout := range []string{
		time.RFC3339,
		"20060102T150405Z",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, errors.Newf("unsupported time format %q", raw)
}

func entryTime(r taskRecord) time.Time {
	for _, key := range []string{"entry", "modified"} {
		if raw, ok := r[key].(string); ok {
			if t, err := parseTaskTime(raw); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

// normalizeRecord converts timestamps in-place to RFC3339 format.
func normalizeRecord(r taskRecord) {
	for _, key := range []string{"entry", "modified", "due", "wait", "end"} {
		raw, ok := r[key].(string)
		if !ok || raw == "" {
			continue
		}
		if parsed, err := parseTaskTime(raw); err == nil {
			r[key] = parsed.UTC().Format(time.RFC3339)
		}
	}
}

func taskRecordToTask(r taskRecord) contract.Task {
	task := contract.Task{
		UUID:        stringValue(r["uuid"]),
		Description: stringValue(r["description"]),
		Status:      stringValue(r["status"]),
		Project:     stringValue(r["project"]),
		Priority:    stringValue(r["priority"]),
		Due:         stringValue(r["due"]),
		Wait:        stringValue(r["wait"]),
		Entry:       stringValue(r["entry"]),
		Modified:    stringValue(r["modified"]),
		End:         stringValue(r["end"]),
		ReminderID:  stringValue(r["reminder_id"]),
		Tags:        normalizeStringSlice(r["tags"]),
	}
	return task
}

func taskRecordsToTasks(records []taskRecord) []contract.Task {
	tasks := make([]contract.Task, 0, len(records))
	for _, r := range records {
		tasks = append(tasks, taskRecordToTask(r))
	}
	return tasks
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func normalizeStringSlice(raw any) []string {
	switch v := raw.(type) {
	case []string:
		out := slices.Clone(v)
		sort.Strings(out)
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		sort.Strings(out)
		return out
	}
	return nil
}

// unavailableStub is returned when the `task` binary is absent.
type unavailableStub struct{ err error }

func (s *unavailableStub) Configure(_ []byte) error                    { return s.err }
func (s *unavailableStub) Description() (string, error)                { return "", s.err }
func (s *unavailableStub) Tools() ([]byte, error)                      { return nil, s.err }
func (s *unavailableStub) CallTool(_ string, _ []byte) ([]byte, error) { return nil, s.err }
func (s *unavailableStub) AddTask(_ contract.AddTaskParams) (contract.Task, error) {
	return contract.Task{}, s.err
}
func (s *unavailableStub) GetTask(_ string) (contract.Task, error) { return contract.Task{}, s.err }
func (s *unavailableStub) UpdateTask(_ contract.UpdateTaskParams) (contract.Task, error) {
	return contract.Task{}, s.err
}
func (s *unavailableStub) DeleteTask(_ string) error { return s.err }
func (s *unavailableStub) ListTasks(_ contract.TaskFilter) ([]contract.Task, error) {
	return nil, s.err
}
func (s *unavailableStub) ListPending(_ contract.TaskFilter) ([]contract.Task, error) {
	return nil, s.err
}
func (s *unavailableStub) ListDue(_ contract.DueFilter) ([]contract.Task, error) {
	return nil, s.err
}
