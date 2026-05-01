package contract

import (
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

// Task represents a Task task record.
type Task struct {
	UUID        string   `json:"uuid"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	Project     string   `json:"project,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Priority    string   `json:"priority,omitempty"`
	Due         string   `json:"due,omitempty"`
	Wait        string   `json:"wait,omitempty"`
	Entry       string   `json:"entry,omitempty"`
	Modified    string   `json:"modified,omitempty"`
	End         string   `json:"end,omitempty"`
	ReminderID  string   `json:"reminder_id,omitempty"`
}

// AddTaskParams holds arguments for creating a new task.
type AddTaskParams struct {
	Description string
	Project     string
	Tags        []string
	Status      string
	Priority    string
	Due         string
	Wait        string
	ReminderID  string
}

// UpdateTaskParams holds arguments for modifying an existing task.
//
// String fields: empty string means "no change". Use the Clear* flags to
// explicitly remove optional fields (project, priority, due, wait, reminder_id).
// Tags: nil means no change; []string{} removes all tags; a non-nil slice
// replaces the full tag set. SetTags must be true whenever Tags should be applied
// (this distinguishes a nil slice meaning "no change" from an explicit empty set).
type UpdateTaskParams struct {
	UUID        string
	Description string
	Project     string
	Status      string
	Priority    string
	Due         string
	Wait        string
	ReminderID  string
	Tags        []string
	SetTags     bool

	ClearProject    bool
	ClearPriority   bool
	ClearDue        bool
	ClearWait       bool
	ClearReminderID bool
}

// TaskFilter is used by ListTasks and ListPending.
type TaskFilter struct {
	Project      string
	Tags         []string
	Status       string
	UpdatedAfter string
	ReminderID   string
}

// DueFilter is used by ListDue.
type DueFilter struct {
	Project string
	Tags    []string
	Before  string // Task or ISO-8601 timestamp; empty means now
}

// TaskIntegrationPlugin is a thin plugin.Plugin wrapper for the task integration.
type TaskIntegrationPlugin struct{ Impl Integration }

func (p *TaskIntegrationPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &IntegrationRPCServer{Impl: p.Impl}, nil
}

func (p *TaskIntegrationPlugin) Client(_ *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &IntegrationRPC{Client: c}, nil
}
