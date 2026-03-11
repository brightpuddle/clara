// Package main is the entry point for the Clara CLI tool.
// It provides commands to control and inspect the running Clara daemon.
package main

import (
"encoding/json"
"fmt"
"net"
"os"
"sort"
"time"

"github.com/alecthomas/kong"
"github.com/brightpuddle/clara/internal/config"
"github.com/brightpuddle/clara/internal/ipc"
)

// CLI defines the top-level command structure parsed by kong.
type CLI struct {
Config string `short:"c" help:"Path to config file." type:"path"`

Agent  AgentCmd  `cmd:"" help:"Manage the Clara agent lifecycle."`
Intent IntentCmd `cmd:"" help:"Manage intents."`
Tool   ToolCmd   `cmd:"" help:"Manage tools."`
}

// ── Agent commands ────────────────────────────────────────────────────────────

// AgentCmd groups agent lifecycle commands.
type AgentCmd struct {
Start  AgentStartCmd  `cmd:"" help:"Start the Clara agent."`
Stop   AgentStopCmd   `cmd:"" help:"Stop the running Clara agent."`
Status AgentStatusCmd `cmd:"" help:"Show agent status and active intents."`
}

// AgentStartCmd starts the agent (or reports if it is already running).
type AgentStartCmd struct{}

func (c *AgentStartCmd) Run(ctx *Context) error {
if isRunning(ctx.SocketPath) {
fmt.Println("clara agent is already running")
return nil
}
fmt.Println("Start the agent with: clarad")
return nil
}

// AgentStopCmd sends a shutdown request to the running agent.
type AgentStopCmd struct{}

func (c *AgentStopCmd) Run(ctx *Context) error {
resp, err := sendRequest(ctx.SocketPath, ipc.Request{Method: ipc.MethodShutdown})
if err != nil {
return fmt.Errorf("agent not reachable: %w", err)
}
fmt.Println(resp.Message)
return nil
}

// AgentStatusCmd shows agent status.
type AgentStatusCmd struct{}

func (c *AgentStatusCmd) Run(ctx *Context) error {
if !isRunning(ctx.SocketPath) {
fmt.Println("clara agent is not running")
return nil
}
resp, err := sendRequest(ctx.SocketPath, ipc.Request{Method: ipc.MethodStatus})
if err != nil {
return fmt.Errorf("status request failed: %w", err)
}
prettyPrint(resp.Data)
return nil
}

// ── Intent commands ───────────────────────────────────────────────────────────

// IntentCmd groups intent management commands.
type IntentCmd struct {
List IntentListCmd `cmd:"" help:"List all active intents."`
Run  IntentRunCmd  `cmd:"" help:"Manually trigger an intent by ID."`
}

// IntentListCmd lists all active intents.
type IntentListCmd struct{}

func (c *IntentListCmd) Run(ctx *Context) error {
resp, err := sendRequest(ctx.SocketPath, ipc.Request{Method: ipc.MethodList})
if err != nil {
return fmt.Errorf("list request failed: %w", err)
}
prettyPrint(resp.Data)
return nil
}

// IntentRunCmd manually triggers an intent by ID.
type IntentRunCmd struct {
ID string `arg:"" help:"Intent ID to run."`
}

func (c *IntentRunCmd) Run(ctx *Context) error {
resp, err := sendRequest(ctx.SocketPath, ipc.Request{
Method: ipc.MethodRun,
Params: map[string]any{"id": c.ID},
})
if err != nil {
return fmt.Errorf("run request failed: %w", err)
}
fmt.Println(resp.Message)
return nil
}

// ── Tool commands ─────────────────────────────────────────────────────────────

// ToolCmd groups tool management commands.
type ToolCmd struct {
List ToolListCmd `cmd:"" help:"List all registered tools."`
}

// ToolListCmd lists all tools registered in the agent, including internal
// services (db, bridge) and tools from connected MCP servers.
type ToolListCmd struct{}

func (c *ToolListCmd) Run(ctx *Context) error {
resp, err := sendRequest(ctx.SocketPath, ipc.Request{Method: ipc.MethodToolList})
if err != nil {
return fmt.Errorf("tool list request failed: %w", err)
}
// Pretty-print as a sorted list of names for readability.
if items, ok := resp.Data.([]any); ok {
names := make([]string, 0, len(items))
for _, item := range items {
if m, ok := item.(map[string]any); ok {
if name, ok := m["name"].(string); ok {
names = append(names, name)
}
}
}
sort.Strings(names)
for _, name := range names {
fmt.Println(name)
}
return nil
}
prettyPrint(resp.Data)
return nil
}

// ── Shared infrastructure ─────────────────────────────────────────────────────

// Context holds shared state passed to all command Run methods.
type Context struct {
SocketPath string
}

func main() {
var cli CLI
ctx := kong.Parse(&cli,
kong.Name("clara"),
kong.Description("Local agentic orchestrator — control the Clara agent."),
kong.UsageOnError(),
)

cfg, err := loadConfig(cli.Config)
if err != nil {
fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
os.Exit(1)
}

err = ctx.Run(&Context{SocketPath: cfg.ControlSocketPath()})
ctx.FatalIfErrorf(err)
}

func loadConfig(path string) (*config.Config, error) {
if path != "" {
return config.Load(path)
}
return config.LoadDefault()
}

// isRunning checks whether the agent control socket is reachable.
func isRunning(socketPath string) bool {
conn, err := net.DialTimeout("unix", socketPath, 500*time.Millisecond)
if err != nil {
return false
}
conn.Close()
return true
}

// sendRequest dials the agent control socket and sends a JSON-encoded request.
func sendRequest(socketPath string, req ipc.Request) (*ipc.Response, error) {
conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
if err != nil {
return nil, err
}
defer conn.Close()
conn.SetDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck

if err := json.NewEncoder(conn).Encode(req); err != nil {
return nil, fmt.Errorf("send request: %w", err)
}
var resp ipc.Response
if err := json.NewDecoder(conn).Decode(&resp); err != nil {
return nil, fmt.Errorf("decode response: %w", err)
}
if resp.Error != "" {
return &resp, fmt.Errorf("agent error: %s", resp.Error)
}
return &resp, nil
}

func prettyPrint(v any) {
if v == nil {
return
}
enc := json.NewEncoder(os.Stdout)
enc.SetIndent("", "  ")
enc.Encode(v) //nolint:errcheck
}
