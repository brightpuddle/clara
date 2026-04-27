# Replace MCP with Native Plugins Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove all MCP infrastructure (commands, servers, dynamic attach) and replace it with a robust native Go plugin management system.

**Architecture:** 
1. Enhance `Registry` and `Supervisor` to support clean unregistration of tools and intents.
2. Expand `pluginLoader` in `cmd/clara/plugins.go` to manage subprocess lifecycles and thread-safe plugin state.
3. Replace `clara mcp` commands with `clara plugin` and implement new IPC handlers for plugin management.
4. Purge all MCP-related code and configuration fields.

**Tech Stack:** Go 1.24+, `hashicorp/go-plugin`, `cobra`, `zerolog`.

---

### Task 1: Enhance Registry for Plugin Unloading

**Files:**
- Modify: `internal/registry/registry.go`
- Test: `internal/registry/registry_test.go`

- [ ] **Step 1: Implement `UnregisterNamespace` in `Registry`**
This method will remove all tools belonging to a namespace (plugin).

```go
// UnregisterNamespace removes all tools that were registered under the given
// namespace prefix (e.g. "shell.").
func (r *Registry) UnregisterNamespace(ns string) {
	if ns == "" {
		return
	}
	prefix := ns + "."
	r.mu.Lock()
	defer r.mu.Unlock()

	// Clear from capabilities
	delete(r.capabilities, ns)
	delete(r.serverNames, ns)
	delete(r.namespaceDescriptions, ns)

	// Clear tools explicitly tracked by server name
	if tools, ok := r.serverTools[ns]; ok {
		for _, toolName := range tools {
			r.deleteToolLocked(toolName)
		}
		delete(r.serverTools, ns)
	}

	// Double check by prefix for any tools not explicitly tracked
	for name := range r.tools {
		if strings.HasPrefix(name, prefix) {
			r.deleteToolLocked(name)
		}
	}
}
```

- [ ] **Step 2: Add unit test for `UnregisterNamespace`**
Verify tools are removed correctly.

```go
func TestUnregisterNamespace(t *testing.T) {
	reg := New(zerolog.Nop())
	reg.Register("plugin1.tool1", func(ctx context.Context, args map[string]any) (any, error) { return nil, nil })
	reg.Register("plugin1.tool2", func(ctx context.Context, args map[string]any) (any, error) { return nil, nil })
	reg.Register("plugin2.tool1", func(ctx context.Context, args map[string]any) (any, error) { return nil, nil })

	if !reg.Has("plugin1.tool1") || !reg.Has("plugin2.tool1") {
		t.Fatal("tools not registered")
	}

	reg.UnregisterNamespace("plugin1")
	if reg.Has("plugin1.tool1") || reg.Has("plugin1.tool2") {
		t.Error("plugin1 tools still exist after unregistration")
	}
	if !reg.Has("plugin2.tool1") {
		t.Error("plugin2 tool was incorrectly removed")
	}
}
```

- [ ] **Step 3: Run tests**
`go test -v ./internal/registry/...`

- [ ] **Step 4: Commit**
`git add internal/registry/ && git commit -m "feat(registry): add UnregisterNamespace for plugin unloading"`

---

### Task 2: Enhance Supervisor for Intent Unregistration

**Files:**
- Modify: `internal/supervisor/supervisor.go`
- Test: `internal/supervisor/supervisor_test.go`

- [ ] **Step 1: Implement `UnregisterIntent` in `Supervisor`**

```go
// UnregisterIntent removes an intent from the supervisor and stops it if running.
func (s *Supervisor) UnregisterIntent(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	intent, ok := s.intents[id]
	if !ok {
		return
	}

	// Stop any running instances/workers
	for _, inst := range intent.instances {
		inst.stop()
	}

	delete(s.intents, id)
	s.log.Info().Str("intent", id).Msg("intent unregistered")
}
```

- [ ] **Step 2: Add unit test for `UnregisterIntent`**

- [ ] **Step 3: Run tests**
`go test -v ./internal/supervisor/...`

- [ ] **Step 4: Commit**
`git add internal/supervisor/ && git commit -m "feat(supervisor): add UnregisterIntent for plugin unloading"`

---

### Task 3: Enhance `pluginLoader` for Lifecycle Management

**Files:**
- Modify: `cmd/clara/plugins.go`

- [ ] **Step 1: Update `pluginLoader` struct and constructor**
Add a map to track active clients and a mutex.

```go
type pluginLoader struct {
	reg *registry.Registry
	sup *supervisor.Supervisor
	cfg *config.Config
	log zerolog.Logger

	mu      sync.Mutex
	clients map[string]*plugin.Client
}

func newPluginLoader(reg *registry.Registry, sup *supervisor.Supervisor, cfg *config.Config, log zerolog.Logger) *pluginLoader {
	return &pluginLoader{
		reg:     reg,
		sup:     sup,
		cfg:     cfg,
		log:     log.With().Str("component", "plugin_loader").Logger(),
		clients: make(map[string]*plugin.Client),
	}
}
```

- [ ] **Step 2: Implement `Unload(name string)`**
Should stop the client and clean up registry/supervisor.

```go
func (l *pluginLoader) Unload(name string) error {
	l.mu.Lock()
	client, ok := l.clients[name]
	delete(l.clients, name)
	l.mu.Unlock()

	if !ok {
		return fmt.Errorf("plugin %q not loaded", name)
	}

	client.Kill()
	l.reg.UnregisterNamespace(name)
	l.sup.UnregisterIntent(name) // Assuming intent ID matches plugin name for now
	
	l.log.Info().Str("name", name).Msg("plugin unloaded")
	return nil
}
```

- [ ] **Step 3: Refactor `loadIntegrations` to use a separate `Load(name string)` method**
Move the loading logic into `Load(name string)` so it can be called individually.

- [ ] **Step 4: Implement `Reload(name string)` and `List() ([]map[string]any)`**
`List` should scan the directory and check if each is in the `clients` map.

- [ ] **Step 5: Commit**
`git add cmd/clara/plugins.go && git commit -m "feat(plugins): implement plugin lifecycle management in loader"`

---

### Task 4: Add `clara plugin` Command and IPC Handlers

**Files:**
- Create: `cmd/clara/plugin.go`
- Modify: `cmd/clara/serve.go` (add IPC handlers)
- Modify: `internal/ipc/ipc.go` (add methods)

- [ ] **Step 1: Add new IPC methods to `internal/ipc/ipc.go`**
Add `MethodPluginList`, `MethodPluginLoad`, `MethodPluginUnload`, `MethodPluginReload`.

- [ ] **Step 2: Implement IPC handlers in `cmd/clara/serve.go`**
Update `buildHandler` to call `loader.Load`, `loader.Unload`, etc.

- [ ] **Step 3: Create `cmd/clara/plugin.go`**
Implement the CLI commands that send these IPC requests.

```go
var pluginCmd = &cobra.Command{
	Use:   "plugin",
	Short: "Manage native Go plugins",
}

var pluginListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all available plugins",
	RunE:  runPluginList,
}
// ... unload, load, reload
```

- [ ] **Step 4: Commit**
`git add cmd/clara/ internal/ipc/ && git commit -m "feat(cli): add clara plugin commands and IPC handlers"`

---

### Task 5: Purge MCP Code and Configuration

**Files:**
- Delete: `cmd/clara/mcp.go`, `cmd/clara/mcpserver.go`, `cmd/clara/mcp_test.go`, `cmd/clara/mcpserver_test.go`, `cmd/clara/mcp_llm_test.go`
- Delete: `internal/mcpserver/` directory
- Delete: `internal/registry/dynamic_attach.go`, `internal/registry/dynamic_attach_test.go`, `internal/registry/mcp_server.go`, `internal/registry/mcp_server_test.go`
- Modify: `internal/config/config.go` (remove fields)
- Modify: `cmd/clara/main.go` (remove commands)
- Modify: `cmd/clara/serve.go` (remove MCP references)

- [ ] **Step 1: Remove MCP commands from `cmd/clara/main.go`**
Remove `rootCmd.AddCommand(mcpCmd)` and `rootCmd.AddCommand(mcpserverCmd)`.

- [ ] **Step 2: Clean up `config.Config`**
Remove `MCPServers`, `LLM`, etc. Update `applyDefaults` and `parse`.

- [ ] **Step 3: Remove MCP logic from `runDaemon` in `cmd/clara/serve.go`**
Remove `addMCPServers`, `attachServer`, etc.

- [ ] **Step 4: Delete files and directories**

- [ ] **Step 5: Verify build**
`go build -o bin/clara ./cmd/clara`

- [ ] **Step 6: Commit**
`git commit -m "refactor: remove all MCP infrastructure and configuration"`

---

### Task 6: Final Verification

- [ ] **Step 1: Ensure no MCP references remain**
Run `grep -r "mcp" .` (excluding vendor/docs) and verify only documentation or irrelevant hits remain.

- [ ] **Step 2: Test plugin loading/unloading with a sample plugin**
If a sample plugin exists in `cmd/integrations/shell`, build it and move it to `~/.config/clara/integrations` to test.

- [ ] **Step 3: Final Commit**
`git commit -m "chore: final cleanup and verification of plugin system"`
