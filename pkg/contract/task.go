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

// TaskIntegration manages Task tasks.
type TaskIntegration interface {
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

type TaskIntegrationRPC struct {
	IntegrationRPC
}

func (g *TaskIntegrationRPC) AddTask(params AddTaskParams) (Task, error) {
	var resp Task
	err := g.Client.Call("Plugin.AddTask", params, &resp)
	return resp, err
}

func (g *TaskIntegrationRPC) GetTask(uuid string) (Task, error) {
	var resp Task
	err := g.Client.Call("Plugin.GetTask", uuid, &resp)
	return resp, err
}

func (g *TaskIntegrationRPC) UpdateTask(params UpdateTaskParams) (Task, error) {
	var resp Task
	err := g.Client.Call("Plugin.UpdateTask", params, &resp)
	return resp, err
}

func (g *TaskIntegrationRPC) DeleteTask(uuid string) error {
	return g.Client.Call("Plugin.DeleteTask", uuid, &struct{}{})
}

func (g *TaskIntegrationRPC) ListTasks(filter TaskFilter) ([]Task, error) {
	var resp []Task
	err := g.Client.Call("Plugin.ListTasks", filter, &resp)
	return resp, err
}

func (g *TaskIntegrationRPC) ListPending(filter TaskFilter) ([]Task, error) {
	var resp []Task
	err := g.Client.Call("Plugin.ListPending", filter, &resp)
	return resp, err
}

func (g *TaskIntegrationRPC) ListDue(filter DueFilter) ([]Task, error) {
	var resp []Task
	err := g.Client.Call("Plugin.ListDue", filter, &resp)
	return resp, err
}

type TaskIntegrationRPCServer struct {
	IntegrationRPCServer
	Impl TaskIntegration
}

func (s *TaskIntegrationRPCServer) AddTask(params AddTaskParams, resp *Task) error {
	var err error
	*resp, err = s.Impl.AddTask(params)
	return err
}

func (s *TaskIntegrationRPCServer) GetTask(uuid string, resp *Task) error {
	var err error
	*resp, err = s.Impl.GetTask(uuid)
	return err
}

func (s *TaskIntegrationRPCServer) UpdateTask(params UpdateTaskParams, resp *Task) error {
	var err error
	*resp, err = s.Impl.UpdateTask(params)
	return err
}

func (s *TaskIntegrationRPCServer) DeleteTask(uuid string, resp *struct{}) error {
	return s.Impl.DeleteTask(uuid)
}

func (s *TaskIntegrationRPCServer) ListTasks(filter TaskFilter, resp *[]Task) error {
	var err error
	*resp, err = s.Impl.ListTasks(filter)
	return err
}

func (s *TaskIntegrationRPCServer) ListPending(filter TaskFilter, resp *[]Task) error {
	var err error
	*resp, err = s.Impl.ListPending(filter)
	return err
}

func (s *TaskIntegrationRPCServer) ListDue(filter DueFilter, resp *[]Task) error {
	var err error
	*resp, err = s.Impl.ListDue(filter)
	return err
}

// Override base Integration methods to route through s.Impl.

func (s *TaskIntegrationRPCServer) Configure(config []byte, resp *struct{}) error {
	return s.Impl.Configure(config)
}

func (s *TaskIntegrationRPCServer) Description(args EmptyArgs, resp *string) error {
	var err error
	*resp, err = s.Impl.Description()
	return err
}

func (s *TaskIntegrationRPCServer) Tools(args EmptyArgs, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.Tools()
	return err
}

func (s *TaskIntegrationRPCServer) CallTool(args CallToolArgs, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.CallTool(args.Name, args.Args)
	return err
}

type TaskIntegrationPlugin struct {
	Impl TaskIntegration
}

func (p *TaskIntegrationPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &TaskIntegrationRPCServer{
		IntegrationRPCServer: IntegrationRPCServer{Impl: p.Impl},
		Impl:                 p.Impl,
	}, nil
}

func (p *TaskIntegrationPlugin) Client(
	b *plugin.MuxBroker,
	c *rpc.Client,
) (interface{}, error) {
	return &TaskIntegrationRPC{IntegrationRPC: IntegrationRPC{Client: c}}, nil
}
