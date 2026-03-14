package taskwarrior

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
)

const Description = "Built-in Taskwarrior MCP server with CRUD, filtering, and due-task helpers."

type Service struct {
	taskPath        string
	availabilityErr error
	log             zerolog.Logger
}

type taskRecord map[string]any

func New(log zerolog.Logger) *Service {
	path, err := exec.LookPath("task")
	if err != nil {
		err = errors.Wrap(err, "task binary not found on PATH")
	}
	return &Service{
		taskPath:        path,
		availabilityErr: err,
		log:             log.With().Str("component", "mcp_taskwarrior").Logger(),
	}
}

func (s *Service) NewServer() *server.MCPServer {
	mcpServer := server.NewMCPServer(
		"clara-taskwarrior",
		"0.1.0",
		server.WithToolCapabilities(true),
	)

	mcpServer.AddTool(mcp.NewTool(
		"task_add",
		mcp.WithDescription("Create a Taskwarrior task and return the created task JSON."),
		mcp.WithString("description", mcp.Required(), mcp.Description("Task description.")),
		mcp.WithString("project", mcp.Description("Optional project name.")),
		mcp.WithArray("tags", mcp.Description("Optional list of tags to assign.")),
		mcp.WithString("status", mcp.Description("Optional initial status (pending or waiting).")),
		mcp.WithString("priority", mcp.Description("Optional priority, e.g. H, M, or L.")),
		mcp.WithString(
			"due",
			mcp.Description("Optional due timestamp in Taskwarrior or ISO-8601 format."),
		),
		mcp.WithString(
			"wait",
			mcp.Description("Optional wait/until timestamp in Taskwarrior or ISO-8601 format."),
		),
	), s.handleTaskAdd)

	mcpServer.AddTool(mcp.NewTool("task_get",
		mcp.WithDescription("Fetch a single task by UUID."),
		mcp.WithString("uuid", mcp.Required(), mcp.Description("Task UUID.")),
	), s.handleTaskGet)

	mcpServer.AddTool(mcp.NewTool(
		"task_update",
		mcp.WithDescription("Update fields on an existing task and return the updated task JSON."),
		mcp.WithString("uuid", mcp.Required(), mcp.Description("Task UUID.")),
		mcp.WithString("description", mcp.Description("Updated task description.")),
		mcp.WithString(
			"project",
			mcp.Description("Updated project name. Use an empty string to clear the project."),
		),
		mcp.WithArray("tags", mcp.Description("Replacement set of task tags.")),
		mcp.WithString(
			"status",
			mcp.Description("Updated status. Supports pending, waiting, and completed."),
		),
		mcp.WithString(
			"priority",
			mcp.Description("Updated priority. Use an empty string to clear priority."),
		),
		mcp.WithString(
			"due",
			mcp.Description("Updated due timestamp. Use an empty string to clear due."),
		),
		mcp.WithString(
			"wait",
			mcp.Description("Updated wait timestamp. Use an empty string to clear wait."),
		),
	), s.handleTaskUpdate)

	mcpServer.AddTool(mcp.NewTool("task_delete",
		mcp.WithDescription("Delete a task by UUID."),
		mcp.WithString("uuid", mcp.Required(), mcp.Description("Task UUID.")),
	), s.handleTaskDelete)

	mcpServer.AddTool(mcp.NewTool("list_tasks",
		mcp.WithDescription("List tasks filtered by project, tags, and status."),
		mcp.WithString("project", mcp.Description("Optional project name filter.")),
		mcp.WithArray("tags", mcp.Description("Optional list of required tags.")),
		mcp.WithString("status", mcp.Description("Optional Taskwarrior status filter.")),
	), s.handleListTasks)

	mcpServer.AddTool(mcp.NewTool("list_pending",
		mcp.WithDescription("List pending tasks filtered by project and tags."),
		mcp.WithString("project", mcp.Description("Optional project name filter.")),
		mcp.WithArray("tags", mcp.Description("Optional list of required tags.")),
	), s.handleListPending)

	mcpServer.AddTool(mcp.NewTool(
		"list_due",
		mcp.WithDescription("List due pending tasks filtered by project and tags."),
		mcp.WithString("project", mcp.Description("Optional project name filter.")),
		mcp.WithArray("tags", mcp.Description("Optional list of required tags.")),
		mcp.WithString(
			"before",
			mcp.Description("Optional upper-bound due timestamp; defaults to now."),
		),
	), s.handleListDue)

	return mcpServer
}

func (s *Service) handleTaskAdd(
	ctx context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	if result := s.ensureAvailable(); result != nil {
		return result, nil
	}
	before, err := s.exportTasks(ctx, nil)
	if err != nil {
		return toolErrorResult("task_add", err), nil
	}

	description, err := requiredStringArg(req, "description")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	args := []string{"add", description}
	args = append(args, writeFieldArgs(req.GetArguments(), nil)...)
	if _, err := s.runTask(ctx, args...); err != nil {
		return toolErrorResult("task_add", err), nil
	}

	after, err := s.exportTasks(ctx, nil)
	if err != nil {
		return toolErrorResult("task_add", err), nil
	}
	created, err := findCreatedTask(before, after)
	if err != nil {
		return toolErrorResult("task_add", err), nil
	}
	return structuredResult(created)
}

func (s *Service) handleTaskGet(
	ctx context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	if result := s.ensureAvailable(); result != nil {
		return result, nil
	}
	uuid, err := requiredStringArg(req, "uuid")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	task, err := s.getTask(ctx, uuid)
	if err != nil {
		return toolErrorResult("task_get", err), nil
	}
	return structuredResult(task)
}

func (s *Service) handleTaskUpdate(
	ctx context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	if result := s.ensureAvailable(); result != nil {
		return result, nil
	}
	uuid, err := requiredStringArg(req, "uuid")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	current, err := s.getTask(ctx, uuid)
	if err != nil {
		return toolErrorResult("task_update", err), nil
	}

	if status, ok := stringArg(req.GetArguments(), "status"); ok && status == "completed" {
		if _, err := s.runTask(ctx, uuid, "done"); err != nil {
			return toolErrorResult("task_update", err), nil
		}
		updated, err := s.getTask(ctx, uuid)
		if err != nil {
			return toolErrorResult("task_update", err), nil
		}
		return structuredResult(updated)
	}

	modifyArgs := []string{uuid, "modify"}
	modifyArgs = append(modifyArgs, writeFieldArgs(req.GetArguments(), current)...)
	if len(modifyArgs) == 2 {
		return mcp.NewToolResultError("task_update requires at least one field to change"), nil
	}
	if _, err := s.runTask(ctx, modifyArgs...); err != nil {
		return toolErrorResult("task_update", err), nil
	}
	updated, err := s.getTask(ctx, uuid)
	if err != nil {
		return toolErrorResult("task_update", err), nil
	}
	return structuredResult(updated)
}

func (s *Service) handleTaskDelete(
	ctx context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	if result := s.ensureAvailable(); result != nil {
		return result, nil
	}
	uuid, err := requiredStringArg(req, "uuid")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if _, err := s.runTask(ctx, uuid, "delete"); err != nil {
		return toolErrorResult("task_delete", err), nil
	}
	return structuredResult(map[string]any{"uuid": uuid, "deleted": true})
}

func (s *Service) handleListTasks(
	ctx context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	return s.handleTaskList(ctx, req.GetArguments(), false, false)
}

func (s *Service) handleListPending(
	ctx context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := cloneArgs(req.GetArguments())
	args["status"] = "pending"
	return s.handleTaskList(ctx, args, false, false)
}

func (s *Service) handleListDue(
	ctx context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args := cloneArgs(req.GetArguments())
	args["status"] = "pending"
	return s.handleTaskList(ctx, args, true, true)
}

func (s *Service) handleTaskList(
	ctx context.Context,
	args map[string]any,
	dueOnly bool,
	useBeforeFilter bool,
) (*mcp.CallToolResult, error) {
	if result := s.ensureAvailable(); result != nil {
		return result, nil
	}

	filters := readFilters(args)
	tasks, err := s.exportTasks(ctx, filters)
	if err != nil {
		return toolErrorResult("list_tasks", err), nil
	}
	if dueOnly {
		before := time.Now().UTC()
		if useBeforeFilter {
			if rawBefore, ok := stringArg(args, "before"); ok &&
				strings.TrimSpace(rawBefore) != "" {
				before, err = parseTaskTime(rawBefore)
				if err != nil {
					return mcp.NewToolResultError(
						fmt.Sprintf("invalid before timestamp: %v", err),
					), nil
				}
			}
		}
		tasks = filterDueTasks(tasks, before)
	}
	return structuredResult(tasks)
}

func (s *Service) ensureAvailable() *mcp.CallToolResult {
	if s.availabilityErr == nil {
		return nil
	}
	return mcp.NewToolResultError(s.availabilityErr.Error())
}

func (s *Service) getTask(ctx context.Context, uuid string) (taskRecord, error) {
	tasks, err := s.exportTasks(ctx, []string{uuid})
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, errors.Newf("task %q not found", uuid)
	}
	return tasks[0], nil
}

func (s *Service) exportTasks(ctx context.Context, filters []string) ([]taskRecord, error) {
	args := append([]string{}, filters...)
	args = append(args, "export")
	output, err := s.runTask(ctx, args...)
	if err != nil {
		return nil, err
	}
	var tasks []taskRecord
	if len(strings.TrimSpace(string(output))) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(output, &tasks); err != nil {
		return nil, errors.Wrap(err, "parse task export json")
	}
	sort.Slice(tasks, func(i, j int) bool {
		return entryTime(tasks[i]).After(entryTime(tasks[j]))
	})
	return tasks, nil
}

func (s *Service) runTask(ctx context.Context, args ...string) ([]byte, error) {
	baseArgs := []string{"rc.json.array=on", "rc.confirmation=no"}
	cmd := exec.CommandContext(ctx, s.taskPath, append(baseArgs, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, errors.Wrapf(
			err,
			"run task command %q: %s",
			strings.Join(args, " "),
			strings.TrimSpace(string(output)),
		)
	}
	return output, nil
}

func writeFieldArgs(args map[string]any, current taskRecord) []string {
	var fields []string
	if description, ok := stringArg(args, "description"); ok {
		fields = append(fields, fmt.Sprintf("description:%s", description))
	}
	if project, ok := stringArg(args, "project"); ok {
		if project == "" {
			fields = append(fields, "project:")
		} else {
			fields = append(fields, fmt.Sprintf("project:%s", project))
		}
	}
	if priority, ok := stringArg(args, "priority"); ok {
		fields = append(fields, fmt.Sprintf("priority:%s", priority))
	}
	if due, ok := stringArg(args, "due"); ok {
		fields = append(fields, fmt.Sprintf("due:%s", due))
	}
	if wait, ok := stringArg(args, "wait"); ok {
		fields = append(fields, fmt.Sprintf("wait:%s", wait))
	}
	if status, ok := stringArg(args, "status"); ok && status != "completed" {
		fields = append(fields, fmt.Sprintf("status:%s", status))
	}
	if rawTags, ok := args["tags"]; ok {
		tags := normalizeStringSlice(rawTags)
		currentTags := normalizeStringSlice(current["tags"])
		for _, tag := range currentTags {
			if !slices.Contains(tags, tag) {
				fields = append(fields, "-"+tag)
			}
		}
		for _, tag := range tags {
			if !slices.Contains(currentTags, tag) {
				fields = append(fields, "+"+tag)
			}
		}
	}
	return fields
}

func readFilters(args map[string]any) []string {
	var filters []string
	if project, ok := stringArg(args, "project"); ok && project != "" {
		filters = append(filters, fmt.Sprintf("project:%s", project))
	}
	for _, tag := range normalizeStringSlice(args["tags"]) {
		filters = append(filters, "+"+tag)
	}
	if status, ok := stringArg(args, "status"); ok && status != "" {
		filters = append(filters, fmt.Sprintf("status:%s", status))
	}
	return filters
}

func filterDueTasks(tasks []taskRecord, before time.Time) []taskRecord {
	filtered := make([]taskRecord, 0, len(tasks))
	for _, task := range tasks {
		dueRaw, ok := task["due"].(string)
		if !ok || strings.TrimSpace(dueRaw) == "" {
			continue
		}
		dueTime, err := parseTaskTime(dueRaw)
		if err != nil {
			continue
		}
		if !dueTime.After(before) {
			filtered = append(filtered, task)
		}
	}
	return filtered
}

func findCreatedTask(before, after []taskRecord) (taskRecord, error) {
	beforeSet := make(map[string]struct{}, len(before))
	for _, task := range before {
		if uuid, _ := task["uuid"].(string); uuid != "" {
			beforeSet[uuid] = struct{}{}
		}
	}
	for _, task := range after {
		uuid, _ := task["uuid"].(string)
		if uuid == "" {
			continue
		}
		if _, ok := beforeSet[uuid]; !ok {
			return task, nil
		}
	}
	return nil, errors.New("unable to identify created task from export output")
}

func parseTaskTime(raw string) (time.Time, error) {
	layouts := []string{time.RFC3339, "20060102T150405Z", "2006-01-02T15:04:05Z07:00", "2006-01-02"}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, errors.Newf("unsupported time format %q", raw)
}

func entryTime(task taskRecord) time.Time {
	for _, key := range []string{"entry", "modified"} {
		if raw, ok := task[key].(string); ok {
			if parsed, err := parseTaskTime(raw); err == nil {
				return parsed
			}
		}
	}
	return time.Time{}
}

func structuredResult(value any) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultStructuredOnly(value), nil
}

func toolErrorResult(tool string, err error) *mcp.CallToolResult {
	return mcp.NewToolResultError(fmt.Sprintf("%s: %v", tool, err))
}

func requiredStringArg(req mcp.CallToolRequest, name string) (string, error) {
	value, ok := stringArg(req.GetArguments(), name)
	if !ok || value == "" {
		return "", errors.Newf("%s argument is required", name)
	}
	return value, nil
}

func stringArg(args map[string]any, name string) (string, bool) {
	value, ok := args[name]
	if !ok {
		return "", false
	}
	str, ok := value.(string)
	return str, ok
}

func normalizeStringSlice(raw any) []string {
	items, ok := raw.([]any)
	if !ok {
		if strings, ok := raw.([]string); ok {
			return slices.Clone(strings)
		}
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if str, ok := item.(string); ok && str != "" {
			result = append(result, str)
		}
	}
	sort.Strings(result)
	return result
}

func cloneArgs(args map[string]any) map[string]any {
	cloned := make(map[string]any, len(args))
	for key, value := range args {
		cloned[key] = value
	}
	return cloned
}
