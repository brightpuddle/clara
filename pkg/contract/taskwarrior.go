package contract

import (
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

// Task represents a Taskwarrior task record.
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
	Before  string // Taskwarrior or ISO-8601 timestamp; empty means now
}

// TaskwarriorIntegration manages Taskwarrior tasks.
type TaskwarriorIntegration interface {
	Integration
	AddTask(params AddTaskParams) (Task, error)
	GetTask(uuid string) (Task, error)
	UpdateTask(params UpdateTaskParams) (Task, error)
	DeleteTask(uuid string) error
	ListTasks(filter TaskFilter) ([]Task, error)
	ListPending(filter TaskFilter) ([]Task, error)
	ListDue(filter DueFilter) ([]Task, error)
}

// --- RPC Wrappers ---

type TaskwarriorIntegrationRPC struct {
	IntegrationRPC
}

func (g *TaskwarriorIntegrationRPC) AddTask(params AddTaskParams) (Task, error) {
	var resp Task
	err := g.Client.Call("Plugin.AddTask", params, &resp)
	return resp, err
}

func (g *TaskwarriorIntegrationRPC) GetTask(uuid string) (Task, error) {
	var resp Task
	err := g.Client.Call("Plugin.GetTask", uuid, &resp)
	return resp, err
}

func (g *TaskwarriorIntegrationRPC) UpdateTask(params UpdateTaskParams) (Task, error) {
	var resp Task
	err := g.Client.Call("Plugin.UpdateTask", params, &resp)
	return resp, err
}

func (g *TaskwarriorIntegrationRPC) DeleteTask(uuid string) error {
	return g.Client.Call("Plugin.DeleteTask", uuid, &struct{}{})
}

func (g *TaskwarriorIntegrationRPC) ListTasks(filter TaskFilter) ([]Task, error) {
	var resp []Task
	err := g.Client.Call("Plugin.ListTasks", filter, &resp)
	return resp, err
}

func (g *TaskwarriorIntegrationRPC) ListPending(filter TaskFilter) ([]Task, error) {
	var resp []Task
	err := g.Client.Call("Plugin.ListPending", filter, &resp)
	return resp, err
}

func (g *TaskwarriorIntegrationRPC) ListDue(filter DueFilter) ([]Task, error) {
	var resp []Task
	err := g.Client.Call("Plugin.ListDue", filter, &resp)
	return resp, err
}

type TaskwarriorIntegrationRPCServer struct {
	IntegrationRPCServer
	Impl TaskwarriorIntegration
}

func (s *TaskwarriorIntegrationRPCServer) AddTask(params AddTaskParams, resp *Task) error {
	var err error
	*resp, err = s.Impl.AddTask(params)
	return err
}

func (s *TaskwarriorIntegrationRPCServer) GetTask(uuid string, resp *Task) error {
	var err error
	*resp, err = s.Impl.GetTask(uuid)
	return err
}

func (s *TaskwarriorIntegrationRPCServer) UpdateTask(params UpdateTaskParams, resp *Task) error {
	var err error
	*resp, err = s.Impl.UpdateTask(params)
	return err
}

func (s *TaskwarriorIntegrationRPCServer) DeleteTask(uuid string, resp *struct{}) error {
	return s.Impl.DeleteTask(uuid)
}

func (s *TaskwarriorIntegrationRPCServer) ListTasks(filter TaskFilter, resp *[]Task) error {
	var err error
	*resp, err = s.Impl.ListTasks(filter)
	return err
}

func (s *TaskwarriorIntegrationRPCServer) ListPending(filter TaskFilter, resp *[]Task) error {
	var err error
	*resp, err = s.Impl.ListPending(filter)
	return err
}

func (s *TaskwarriorIntegrationRPCServer) ListDue(filter DueFilter, resp *[]Task) error {
	var err error
	*resp, err = s.Impl.ListDue(filter)
	return err
}

// Override base Integration methods to route through s.Impl.

func (s *TaskwarriorIntegrationRPCServer) Configure(config []byte, resp *struct{}) error {
	return s.Impl.Configure(config)
}

func (s *TaskwarriorIntegrationRPCServer) Description(args EmptyArgs, resp *string) error {
	var err error
	*resp, err = s.Impl.Description()
	return err
}

func (s *TaskwarriorIntegrationRPCServer) Tools(args EmptyArgs, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.Tools()
	return err
}

func (s *TaskwarriorIntegrationRPCServer) CallTool(args CallToolArgs, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.CallTool(args.Name, args.Args)
	return err
}

type TaskwarriorIntegrationPlugin struct {
	Impl TaskwarriorIntegration
}

func (p *TaskwarriorIntegrationPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &TaskwarriorIntegrationRPCServer{
		IntegrationRPCServer: IntegrationRPCServer{Impl: p.Impl},
		Impl:                 p.Impl,
	}, nil
}

func (p *TaskwarriorIntegrationPlugin) Client(
	b *plugin.MuxBroker,
	c *rpc.Client,
) (interface{}, error) {
	return &TaskwarriorIntegrationRPC{IntegrationRPC: IntegrationRPC{Client: c}}, nil
}
