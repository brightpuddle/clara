# Design Spec: Clara Plugin System (MCP Replacement)

**Date:** 2026-04-26
**Status:** Draft
**Topic:** Replacing MCP infrastructure with a native Go plugin system.

## 1. Objective

Clara is moving away from the Model Context Protocol (MCP) for managed servers. All built-in and configured MCP servers will be removed and replaced by a native Go plugin system using `hashicorp/go-plugin`. This simplifies the architecture, improves performance, and provides a more stable interface for integrations.

## 2. Removals

### 2.1 CLI Commands
*   Remove `clara mcp` and all subcommands (`list`, `start`, `stop`, `restart`, `add`, `remove`).
*   Remove `clara mcpserver` and all built-in server launchers (`db`, `llm`, `shell`, etc.).

### 2.2 Daemon Infrastructure
*   **Registry:** Remove `MCPServer` management, including `AddServer`, `StartServers`, `StopServers`, `WaitReady`, and `RestartServer`. The `Registry` will remain as a central hub for `Tool` definitions but will no longer manage the lifecycle of the processes providing them.
*   **Built-in Servers:** Delete the entire `internal/mcpserver/` directory.
*   **Dynamic MCP:** Remove `internal/registry/dynamic_attach.go` and the `DynamicAttachServer`. The TUI (which previously used this) will be integrated natively.
*   **IPC:** Remove all IPC methods prefixed with `mcp.` (`mcp.list`, `mcp.start`, `mcp.stop`, `mcp.add`, etc.).

### 2.3 Configuration
*   Remove the following fields from `config.Config`:
    *   `MCPServers`
    *   `MCPCommandSearchPaths`
    *   `StdioMCP`
    *   `LLM`
    *   `MCPStartupTimeout`

## 3. Native Plugin System

### 3.1 Plugin Discovery
*   Plugins are standalone binaries located in `~/.config/clara/integrations`.
*   The daemon scans this directory at startup and attempts to load all non-hidden files.

### 3.2 `pluginLoader` Enhancements
The `pluginLoader` in `cmd/clara/plugins.go` will be expanded to manage the lifecycle of plugins:
*   **State:** Maintain a thread-safe map of active plugin clients (`name -> client`).
*   **Load(name):**
    1. Find the binary in the integrations directory.
    2. Start the subprocess using `go-plugin`.
    3. Negotiate the RPC interface.
    4. Register tools/intents into the `Registry`.
*   **Unload(name):**
    1. Signal the plugin subprocess to exit.
    2. Clean up RPC resources.
    3. **Remove all tools associated with this plugin from the `Registry`.**
*   **Reload(name):** Sequential `Unload` and `Load`.
*   **List():** Return a list of all binaries in the integrations directory and their current status (Loaded/Unloaded).

### 3.3 CLI: `clara plugin` Command
A new `clara plugin` command will be added with the following subcommands:
*   `clara plugin list`: Shows available and active plugins.
*   `clara plugin load <name>`: Manually loads a plugin.
*   `clara plugin unload <name>`: Manually unloads a plugin.
*   `clara plugin reload <name>`: Reloads a plugin.

## 4. IPC Interface

The following new IPC methods will be added:
*   `plugin.list`: Returns list of plugins and statuses.
*   `plugin.load`: Parameters: `{"name": "plugin-name"}`.
*   `plugin.unload`: Parameters: `{"name": "plugin-name"}`.
*   `plugin.reload`: Parameters: `{"name": "plugin-name"}`.

## 5. Registry Changes

The `Registry` needs a way to remove tools by namespace or source to support plugin unloading.
*   Add/Expose `UnregisterNamespace(namespace string)` to remove all tools starting with `namespace.`.

## 6. Testing Strategy

*   **Unit Tests:** Verify `pluginLoader` can load and unload a mock plugin.
*   **Integration Tests:** Ensure `clara plugin` commands correctly communicate with the daemon and update the tool registry.
*   **Verification:** Ensure no `mcp` commands remain and `config.yaml` can be parsed without the removed fields.
