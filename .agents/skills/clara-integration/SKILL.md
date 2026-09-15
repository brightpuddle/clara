---
name: clara-integration
description:
  Guide and templates for creating or modifying Clara native Go integration plugins (hashicorp/go-plugin) and MCP tool providers. Use when implementing or updating integrations in cmd/integrations/.
---

# Clara Integration Plugin Guide

This skill provides standard patterns and best practices for creating and maintaining native Go integration plugins for Clara.

---

## Architecture Overview

Clara integrations are standalone Go binaries located in `cmd/integrations/<name>/`. They communicate with the Clara daemon over standard RPC using `hashicorp/go-plugin`.

Integrations provide:
1. **Tool Definitions (`Tools()`)**: MCP tool schemas (`mcp.Tool`) exposed to the LLM and CLI.
2. **Tool Execution (`CallTool()`)**: Handler routing JSON tool arguments to plugin logic.
3. **Optional Event Streaming (`StreamEvents()`)**: Push real-time CloudEvents onto the Clara event bus.
4. **Optional HTTP Webhook Handling (`HandleHTTP()`)**: Inbound webhook endpoints routed via Clara's HTTP server.

> **Important:** Integration plugins must NEVER write logs or text to `stdout` because `stdout` is reserved for `go-plugin` RPC framing. Use `os.Stderr` or `zerolog.New(os.Stderr)`.

---

## Integration Directory Structure

```text
cmd/integrations/<name>/
  ├── main.go          # Plugin entrypoint and go-plugin.Serve configuration
  ├── <name>.go        # Integration struct implementing contract.Integration
  └── <name>_test.go   # Integration tests
pkg/contract/
  ├── <name>.go        # Plugin wrapper and typed contract structs
  └── contract.go      # Base interfaces
```

---

## Step-by-Step Implementation Workflow

### 1. Define the Contract in `pkg/contract/<name>.go`

Create the plugin wrapper struct implementing `plugin.Plugin`:

```go
package contract

import (
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

// <Name>IntegrationPlugin is a thin plugin.Plugin wrapper.
type <Name>IntegrationPlugin struct {
	Impl Integration
}

func (p *<Name>IntegrationPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &IntegrationRPCServer{Impl: p.Impl}, nil
}

func (p *<Name>IntegrationPlugin) Client(_ *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &IntegrationRPC{Client: c}, nil
}
```

### 2. Implement the Plugin Struct in `cmd/integrations/<name>/<name>.go`

Implement `contract.Integration` (`Configure`, `Description`, `Tools`, `CallTool`):

```go
package main

import (
	"encoding/json"
	"os"

	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"
)

const pluginDescription = "Description of your integration"

type Config struct {
	ApiKey string `json:"api_key"`
}

type Integration struct {
	cfg Config
	log zerolog.Logger
}

func newIntegration() *Integration {
	return &Integration{
		log: zerolog.New(os.Stderr).With().Timestamp().Logger(),
	}
}

func (i *Integration) Configure(configData []byte) error {
	if len(configData) > 0 {
		if err := json.Unmarshal(configData, &i.cfg); err != nil {
			return errors.Wrap(err, "parse integration config")
		}
	}
	return nil
}

func (i *Integration) Description() (string, error) {
	return pluginDescription, nil
}

func (i *Integration) Tools() ([]byte, error) {
	tools := []mcp.Tool{
		mcp.NewTool(
			"do_action",
			mcp.WithDescription("Performs an action in the external system."),
			mcp.WithString("target", mcp.Required(), mcp.Description("Target resource name")),
		),
	}
	return json.Marshal(tools)
}

func (i *Integration) CallTool(name string, args []byte) ([]byte, error) {
	switch name {
	case "do_action":
		var params struct {
			Target string `json:"target"`
		}
		if err := json.Unmarshal(args, &params); err != nil {
			return nil, errors.Wrap(err, "invalid arguments")
		}
		res := map[string]any{"status": "ok", "target": params.Target}
		return json.Marshal(res)
	default:
		return nil, errors.Newf("unknown tool: %q", name)
	}
}
```

### 3. Create Entrypoint in `cmd/integrations/<name>/main.go`

```go
package main

import (
	"github.com/brightpuddle/clara/pkg/contract"
	"github.com/hashicorp/go-plugin"
)

func main() {
	impl := newIntegration()
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: contract.HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"<name>": &contract.<Name>IntegrationPlugin{Impl: impl},
		},
	})
}
```

### 4. Register Plugin in Daemon Host (`cmd/clara/plugins.go`)

Add your plugin to `pluginMap` inside `loadIntegrationAt`:

```go
pluginMap := map[string]plugin.Plugin{
	"chrome":  &contract.ChromeIntegrationPlugin{},
	"zk":      &contract.ZkIntegrationPlugin{},
	"<name>":  &contract.<Name>IntegrationPlugin{}, // ← Add here
}
```

### 5. Add to Config (`~/.config/clara/config.yaml`)

```yaml
plugins:
  - name: <name>

integrations:
  <name>:
    api_key: "secret"
```

---

## Tool Naming & Design Best Practices

- Tool names exposed to Clara registry are automatically prefixed with the plugin namespace: `<plugin_name>.<tool_name>` (e.g. `zk.note_list`, `task.create`).
- Use concise `snake_case` tool names (`list`, `get`, `search`, `create`, `update`, `delete`).
- Use `mark3labs/mcp-go/mcp` builders for schema validation and clear parameter descriptions.
- Test your tool directly using the CLI:
  ```bash
  clara tool list <name>
  clara tool show <name>.<tool_name>
  clara tool call <name>.<tool_name> -a '{"key": "value"}'
  ```
- Regenerate TypeScript types whenever tool signatures change:
  ```bash
  clara types gen
  ```
