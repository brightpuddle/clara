# Clara Plugin Integration Architecture

**Goal:** Migrate existing dynamic MCP integrations to statically typed, compiled Go plugins via `hashicorp/go-plugin` to provide complete type safety for native Go intents while still serving dynamic tools to the LLM.

## Architecture

The migration from MCP to Go Plugins revolves around defining static typed contracts that define the interface for each integration, alongside a unified base interface to manage the lifecycle and the dynamic aspects required by Clara's LLM integration.

### 1. Configuration Management (`internal/config`)

Clara's configuration will be updated to replace generic `mcp_servers` configuration with a typed map of integration settings. Each integration gets its own named configuration block in `config.yaml`. 

```yaml
integrations:
  zk:
    paths: ["~/notes"]
  shell: {}
```

The daemon reads this config and passes each specific sub-block down to the respective plugin subprocess via an RPC `Configure` call.

### 2. The Base Contract (`pkg/contract/contract.go`)

A base `Integration` interface will define the common lifecycle methods required for all plugins, including configuration injection and dynamic tool exposure to the LLM.

```go
type Integration interface {
    // Lifecycle
    Configure(config []byte) error
    
    // Dynamic LLM Tool Registration
    Tools() ([]mcp.Tool, error)
    CallTool(name string, args []byte) ([]byte, error)
}
```

### 3. Specific Contracts (`pkg/contract/`)

Each integration will embed the base `Integration` interface into a new, strongly-typed interface. These typed interfaces define the methods that native Go intents can invoke directly.

```go
type ZKIntegration interface {
    Integration
    SearchNotes(query string) ([]Note, error)
    CreateNote(title string, content string) error
}
```

### 4. Plugin Loader (`internal/mcpserver/plugin_loader.go`)

The daemon will manage integrations as separate processes. For each integration defined in the configuration or discovered in the `~/.config/clara/integrations/` directory:

1. **Launch:** Start the plugin subprocess via `exec.Command`.
2. **Connect:** Establish the RPC connection over `hashicorp/go-plugin`.
3. **Configure:** Look up the integration's specific configuration blob from the global config, serialize it to JSON/YAML, and pass it via the `Configure([]byte)` RPC method.
4. **Register Tools:** Invoke `Tools()` to receive a list of dynamic JSON schemas representing the plugin's capabilities.
5. **Bind:** Register each tool with the central tool registry so the LLM can trigger them via the `CallTool(name string, args []byte)` RPC method.

### 5. Migration Strategy

To safely and consistently migrate existing MCP servers:
1. Define the specific contract in `pkg/contract/`.
2. Implement the interface in a standalone Go `main` package (the plugin).
3. Connect the plugin implementation to `hashicorp/go-plugin` RPC wrappers.
4. Update `plugin_loader.go` to handle the new plugin identifier.
5. Remove the old MCP server implementation.

---

## Data Flow & Error Handling

- **Validation:** Plugins will perform configuration validation inside the `Configure` method. Any validation errors will be returned over RPC to Clara, which will log the error and mark the plugin as degraded/failed.
- **LLM Invocations:** The LLM receives standard JSON schema definitions via the `Tools()` method. When the LLM decides to invoke a tool, Clara calls `CallTool(name, jsonPayload)`. The plugin parses the payload, executes the internal logic, and returns a stringified JSON response or an error string to Clara.
- **Intent Invocations:** Native Go intents will request a specific plugin (e.g., `ZKIntegration`) from the registry, receive the typed RPC client, and call the explicit Go methods like `SearchNotes(query)`. Type safety is guaranteed at compile time for the intents.

## Testing & Verification

- **Mockability:** The base and specific `Integration` interfaces can be mocked using standard Go mock generators for unit testing both the Clara daemon and the native intents.
- **Plugin Testing:** Plugins can be tested directly in Go without starting the RPC server by simply instantiating the struct and invoking its methods.
