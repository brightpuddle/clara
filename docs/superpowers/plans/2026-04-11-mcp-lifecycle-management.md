# MCP Lifecycle Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add granular control for managed MCP servers, simplify configuration to a single command string, and restructure the CLI.

**Architecture:** 
- Break backward compatibility in `MCPServerConfig` by replacing `Command` and `Args` with a single `Command` string.
- Update `Registry` to manage individual server lifecycles and track status.
- Repurpose `clara mcp` for management and `clara mcpserver` for launching built-ins.
- Implement config persistence in the daemon.

**Tech Stack:** Go, Cobra, zerolog, gopkg.in/yaml.v3, github.com/google/shlex (to be added).

---

### Task 1: Add Dependencies & Update Config Struct

**Files:**
- Modify: `go.mod`
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Add `github.com/google/shlex` dependency**

Run: `go get github.com/google/shlex`

- [ ] **Step 2: Update `MCPServerConfig` struct and parsing logic**

Remove `Args` field. Update `MCPServerConfig` to just use `Command`.

```go
type MCPServerConfig struct {
	Name        string            `yaml:"name"`
	URL         string            `yaml:"url"`
	Token       string            `yaml:"token"`
	SkipVerify  bool              `yaml:"skip_verify"`
	Command     string            `yaml:"command"` // Full command string now
	Env         map[string]string `yaml:"env"`
	Description string            `yaml:"description"`
}
```

Update `internal/config/config.go` to use `shlex.Split` when building the command for execution (though this happens in the registry, we should ensure the config structure is ready).

- [ ] **Step 3: Update `config_test.go` to match new structure**

Fix any tests that rely on the `Args` field.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/config/...`

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/config/
git commit -m "config: simplify MCPServerConfig and add shlex dependency"
```

---

### Task 2: Update `MCPServer` for Lifecycle & Status

**Files:**
- Modify: `internal/registry/mcp_server.go`
- Modify: `internal/registry/registry.go`

- [ ] **Step 1: Add Status field and enums to `MCPServer`**

```go
type ServerStatus string
const (
	StatusStopped    ServerStatus = "STOPPED"
	StatusConnecting ServerStatus = "CONNECTING"
	StatusRunning    ServerStatus = "RUNNING"
	StatusFailed     ServerStatus = "FAILED"
)

type MCPServer struct {
    // ...
    status ServerStatus
    cancel context.CancelFunc // To stop the server
    // ...
}
```

- [ ] **Step 2: Implement `Start` and `Stop` in `MCPServer`**

Update `Start` to set status and handle command splitting using `shlex`.
Update `Stop` to cancel context and close client.

- [ ] **Step 3: Add Lifecycle methods to `Registry`**

- `StartServer(name string)`
- `StopServer(name string)`
- `RestartServer(name string)`
- `ServerStatuses() map[string]ServerStatus`

- [ ] **Step 4: Implement Tool Cleanup on Stop**

When `StopServer` is called, remove all tools from `r.tools` that match `name.*`.

- [ ] **Step 5: Commit**

```bash
git add internal/registry/
git commit -m "registry: implement server lifecycle and status tracking"
```

---

### Task 3: IPC Protocol Extensions

**Files:**
- Modify: `internal/ipc/ipc.go`

- [ ] **Step 1: Add new IPC method constants**

```go
const (
	// ...
	MethodMCPStart   = "mcp.start"
	MethodMCPStop    = "mcp.stop"
	MethodMCPRestart = "mcp.restart"
	MethodMCPAdd     = "mcp.add"
	MethodMCPRemove  = "mcp.remove"
	// MethodMCPList is already there, but we'll repurpose its behavior
)
```

- [ ] **Step 2: Commit**

```bash
git add internal/ipc/ipc.go
git commit -m "ipc: add MCP lifecycle management methods"
```

---

### Task 4: Daemon Handler Updates

**Files:**
- Modify: `cmd/clara/serve.go`

- [ ] **Step 1: Update `buildHandler` to handle new MCP methods**

Implement `MethodMCPList`, `MethodMCPStart`, `MethodMCPStop`, `MethodMCPRestart`, `MethodMCPAdd`, `MethodMCPRemove`.

- [ ] **Step 2: Implement `MethodMCPAdd` with overwrite logic**

- [ ] **Step 3: Implement Config Persistence**

Add a function to save the current `Config` back to `config.yaml`.

- [ ] **Step 4: Commit**

```bash
git add cmd/clara/serve.go
git commit -m "serve: implement MCP lifecycle IPC handlers and persistence"
```

---

### Task 5: Move Built-in Servers to `mcpserver`

**Files:**
- Create: `cmd/clara/mcpserver.go`
- Modify: `cmd/clara/mcp.go`
- Modify: `cmd/clara/main.go`

- [ ] **Step 1: Create `mcpserver.go` and move subcommands**

Move `fs`, `db`, `zk`, `shell`, etc., from `mcpCmd` to a new `mcpserverCmd`.

- [ ] **Step 2: Update `main.go` to register `mcpserverCmd`**

- [ ] **Step 3: Update `mcp.go` to use the new management commands**

Repurpose `mcpCmd` to have `list`, `start`, `stop`, `restart`, `add`, `remove` subcommands.

- [ ] **Step 4: Commit**

```bash
git add cmd/clara/
git commit -m "cli: restructure mcp commands and add mcpserver parent"
```

---

### Task 6: Final Integration & Verification

- [ ] **Step 1: Verify `clara mcp list`**
- [ ] **Step 2: Verify `clara mcp stop <name>` and `start <name>`**
- [ ] **Step 3: Verify `clara mcp add` persists to config.yaml**
- [ ] **Step 4: Verify built-in commands work under `clara mcpserver`**

- [ ] **Step 5: Commit final changes**

```bash
git commit -m "feat: complete MCP lifecycle management implementation"
```
