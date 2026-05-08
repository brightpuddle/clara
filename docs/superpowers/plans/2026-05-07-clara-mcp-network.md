# Clara MCP Network Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate Webex and Discord into Clara plugins and expose Clara as an MCP server with event streaming.

**Architecture:** We will create an HTTP server in Clara that natively serves the MCP protocol over SSE, forwards remote events, and proxies webhook requests to plugins. Webex and Discord will be moved into self-contained `hashicorp/go-plugin` integrations. Tools discovered from remote Clara instances will merge into the local root namespace.

**Tech Stack:** Go, Echo/Net/HTTP, hashicorp/go-plugin, mark3labs/mcp-go.

---

### Task 1: Add HTTP Support to Plugin Contract

**Files:**
- Modify: `pkg/contract/contract.go`
- Modify: `cmd/clara/plugins.go`

- [ ] **Step 1: Define `HTTPIntegration` interface in `contract.go`**

```go
// HTTPIntegration is an optional interface for plugins that handle HTTP requests (webhooks/OAuth).
type HTTPIntegration interface {
	HandleHTTP(method, path string, body []byte) (status int, resp []byte, err error)
}
```

- [ ] **Step 2: Add RPC wrapper methods for `HTTPIntegration` in `contract.go`**

Add the necessary `HandleHTTP` methods to `IntegrationRPC` and `IntegrationRPCServer` (or create new ones if needed, though extending `Integration` with a dummy default is sometimes easier. To avoid breaking changes, add it as a separate optional RPC capability or just add it to `Integration` and update existing plugins).
Since we have few plugins, let's just add `HandleHTTP(method, path string, body []byte) (int, []byte, error)` directly to the `Integration` interface in `contract.go` and provide a dummy implementation in other plugins if necessary, OR implement a `contract.HTTPHandler` struct that can be embedded. Let's assume we add it to `Integration` and update the empty/dummy implementations in existing plugins.

*Wait*, we can use a simpler approach: use `CallTool("webhook", body)` or similar. Let's stick to adding `HandleHTTP` to `Integration` for simplicity, or adding an optional interface check.

```go
type HTTPRequest struct {
	Method string
	Path   string
	Body   []byte
}

type HTTPResponse struct {
	Status int
	Body   []byte
}

func (g *IntegrationRPC) HandleHTTP(method, path string, body []byte) (int, []byte, error) {
	req := HTTPRequest{Method: method, Path: path, Body: body}
	var resp HTTPResponse
	err := g.Client.Call("Plugin.HandleHTTP", req, &resp)
	return resp.Status, resp.Body, err
}

func (s *IntegrationRPCServer) HandleHTTP(req HTTPRequest, resp *HTTPResponse) error {
	if httpImpl, ok := s.Impl.(HTTPIntegration); ok {
		status, body, err := httpImpl.HandleHTTP(req.Method, req.Path, req.Body)
		resp.Status = status
		resp.Body = body
		return err
	}
	resp.Status = 404
	resp.Body = []byte("Not Implemented")
	return nil
}
```

- [ ] **Step 3: Expose plugin HTTP handling in `pluginLoader`**
In `cmd/clara/plugins.go`, add a method to dispatch HTTP requests to a plugin by name.

```go
func (l *pluginLoader) DispatchHTTP(pluginName, method, path string, body []byte) (int, []byte, error) {
    // Look up client, get IntegrationRPC, call HandleHTTP
}
```

---

### Task 2: Create Clara HTTP/MCP Server

**Files:**
- Create: `internal/server/server.go`
- Modify: `internal/config/config.go`

- [ ] **Step 1: Add server configuration**
In `config.go`, add `Server` struct with `ListenAddr` (e.g., `:4444`), `PublicURL` (e.g., `https://eve.com`), and `SharedSecret`.

- [ ] **Step 2: Create `server.go`**
Create an HTTP server using `echo` or standard `net/http` that provides:
- `/mcp/sse` and `/mcp/messages` using `mcp-go/server`.
- `/events` which streams `contract.Event` using SSE (subscribing to `registry.Registry.Subscribe`).
- `/api/:plugin/*` which parses the plugin name and proxies the request to `pluginLoader.DispatchHTTP()`.

---

### Task 3: Migrate Webex to Clara

**Files:**
- Modify: `cmd/integrations/webex/webex.go`
- Modify: `cmd/integrations/webex/config.go`
- Remove `eve/main/internal/handlers/webex.go`

- [ ] **Step 1: Implement `HTTPIntegration` in Webex**
Update `Webex` struct in `webex.go` to implement `HandleHTTP`. Move OAuth flow (`/auth/webex`) and webhook handling (`/api/webex/callback`) from `eve` into this method.

- [ ] **Step 2: Remove Eve dependency**
Webex `CallTool` methods (like `message.reply`) should now execute API calls directly to Webex using the local token manager, instead of proxying to `eve`.

---

### Task 4: Migrate Discord to Clara

**Files:**
- Modify: `cmd/integrations/discord/discord.go`
- Remove `eve/main/internal/handlers/discord.go`

- [ ] **Step 1: Implement `HTTPIntegration` in Discord**
Update `Discord` struct to implement `HandleHTTP`. Move webhook and approval handling from `eve` into this method.

- [ ] **Step 2: Remove Eve dependency**
Discord `CallTool` methods should execute directly using the bot token, instead of proxying to `eve`.

---

### Task 5: Remote Tool Namespacing

**Files:**
- Modify: `internal/registry/registry.go`
- Modify: `internal/registry/mcp_registry.go`

- [ ] **Step 1: Update Namespace Merging**
Modify `GetFQToolName` or `RegisterConnectedClient` so that if a remote server specifies a flag (or by default for Clara-to-Clara), tools are registered without the `serverName.` prefix (i.e. merged into the root namespace).

- [ ] **Step 2: Implement Conflict Resolution**
Modify `Get(name string)` in `Registry` to prioritize local tools over remote tools if there is a naming collision in the root namespace.