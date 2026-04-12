# Design Spec: MCP Lifecycle Management and Configuration Simplification

**Date:** 2026-04-11
**Status:** Approved
**Topic:** Adding granular control for managed MCP servers and simplifying configuration.

## Background
Clara currently manages MCP servers defined in `config.yaml` by starting them all at once when the daemon boots. However, there is no way to individually restart, stop, or add new servers without restarting the entire Clara agent. Additionally, the configuration for MCP servers separates `command` and `args`, which is less intuitive for CLI-based management.

## Goals
1.  **Granular Control:** Add `clara mcp <start|stop|restart|list|add|remove>` commands to manage servers managed by the Clara daemon.
2.  **Configuration Simplification:** Replace the split `command` and `args` fields in `config.yaml` with a single `command` string.
3.  **CLI Restructuring:** Move built-in MCP server launch commands from `clara mcp <name>` to `clara mcpserver <name>` to free up the `clara mcp` namespace for management.
4.  **Observability:** Implement a `list` command that shows the status (Running, Stopped, Failed, Connecting) of all managed servers.
5.  **Dynamic Updates:** Enable adding/removing servers from the running daemon and persisting those changes to `config.yaml`.

## Design

### 1. Configuration Changes (`internal/config`)
The `MCPServerConfig` struct will be updated (breaking change):
- Remove `Args []string`.
- `Command string` now represents the full command line (e.g., `npx -y @modelcontextprotocol/server-github`).
- Parsing will use a shell-split library to correctly handle arguments and quoted paths.

### 2. Registry & Lifecycle (`internal/registry`)
- **`MCPServer` Status:** Add a status field (e.g., an enum/string) to track the process state.
- **Process Management:** Update `MCPServer` to support being stopped and started multiple times.
- **Tool Cleanup:** When a server is stopped, the `Registry` must purge all tools associated with that server (e.g., `github.create_issue`).
- **Registry Methods:** Add `StartServer(name)`, `StopServer(name)`, `RestartServer(name)`, and `ServerStatuses()`.

### 3. IPC Protocol (`internal/ipc`)
New methods for the control socket:
- `mcp.list`: Returns a list of managed servers and their metadata/status.
- `mcp.start`, `mcp.stop`, `mcp.restart`: Target a server by name.
- `mcp.add`: Parameters: `name`, `command`, `description`, `overwrite` (bool).
- `mcp.remove`: Parameters: `name`.

### 4. CLI Commands (`cmd/clara`)
- **`clara mcp` (Management):**
    - `list`: Displays a table of servers.
    - `start <name>`, `stop <name>`, `restart <name>`.
    - `add <name> --command "..."`: Checks for existence, prompts for overwrite if needed, then adds and starts.
    - `remove <name>`: Stops and removes from config.
- **`clara mcpserver` (Built-in Launchers):**
    - `fs`, `db`, `zk`, `shell`, etc. (Moves from `clara mcp <name>`).

### 5. Persistence Workflow
When `clara mcp add` or `clara mcp remove` is called:
1.  The daemon updates its in-memory `Config`.
2.  The daemon re-serializes the `Config` to `~/.config/clara/config.yaml`.
3.  The daemon applies the change (starts the new server or stops/removes the old one).

## User Interaction
- **Overwrite Prompt:** If `add` targets an existing server name:
  `Server "github" already exists. Overwrite? [y/N]`
  (Accepts single keypress `y` or `n`).
- **List Output:**
  ```
  NAME    STATUS      TYPE     COMMAND
  fs      RUNNING     stdio    clara mcpserver fs --watch-path .
  github  FAILED      stdio    npx -y @modelcontextprotocol/server-github
  chrome  RUNNING     http     http://localhost:12306/mcp
  ```

## Testing Strategy
1.  **Unit Tests:** Verify shell-splitting of command strings in `internal/config`.
2.  **Registry Tests:** Verify tool registration/unregistration on server start/stop.
3.  **Integration Tests:** Use the IPC handler to simulate adding/removing servers and verify `config.yaml` persistence.
4.  **CLI Tests:** Verify the new command structure and table formatting.
