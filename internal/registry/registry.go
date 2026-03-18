// Package registry manages the Clara tool registry. It is the central hub that
// holds all Tool implementations and manages the lifecycle of MCP server
// subprocesses.
//
// Tools are registered by name and invoked by the interpreter. The naming
// convention is "server.method" (e.g. "filesystem.read_file"). The registry
// also owns the MCP client connections to each configured server.
package registry

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"
)

// Tool is the standard callable unit in Clara. Every capability — MCP tool
// call, Swift bridge call, local SQLite query — is wrapped as a Tool.
type Tool func(ctx context.Context, args map[string]any) (any, error)

// ToolInfo holds a tool's name and description for display purposes.
type ToolInfo struct {
	Name        string
	Description string
	Spec        mcp.Tool
	Examples    []string
}

type NotificationHandler func(serverName, method string, params any)

// ServerCapabilities holds the full capability set discovered from an MCP server.
type ServerCapabilities struct {
	// Name is the registry alias for this server.
	Name string
	// Description is the human-readable summary (from config or built-in).
	Description string
	// Tools is the complete list of tools, including input schemas.
	Tools []mcp.Tool
	// Resources is the list of static resources exposed by the server.
	Resources []mcp.Resource
	// Prompts is the list of prompt templates exposed by the server.
	Prompts []mcp.Prompt
}

// Registry holds the set of available Tools and MCP server managers.
type Registry struct {
	mu            sync.RWMutex
	tools         map[string]Tool
	descriptions  map[string]string
	specs         map[string]mcp.Tool
	examples      map[string][]string
	servers       []*MCPServer
	serverNames   map[string]struct{}
	dynamic       map[string]dynamicServer
	capabilities  map[string]*ServerCapabilities
	notifications []NotificationHandler
	log           zerolog.Logger
}

type dynamicServer struct {
	close func() error
}

// New creates an empty Registry.
func New(log zerolog.Logger) *Registry {
	return &Registry{
		tools:        make(map[string]Tool),
		descriptions: make(map[string]string),
		specs:        make(map[string]mcp.Tool),
		examples:     make(map[string][]string),
		serverNames:  make(map[string]struct{}),
		dynamic:      make(map[string]dynamicServer),
		capabilities: make(map[string]*ServerCapabilities),
		log:          log,
	}
}

// Subscribe registers a callback for MCP notifications.
func (r *Registry) Subscribe(handler NotificationHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.notifications = append(r.notifications, handler)
}

// EmitNotification triggers notification handlers manually.
// Primarily used for testing or built-in event sources.
func (r *Registry) EmitNotification(serverName, method string, params any) {
	r.mu.RLock()
	handlers := append([]NotificationHandler(nil), r.notifications...)
	r.mu.RUnlock()
	for _, handler := range handlers {
		handler(serverName, method, params)
	}
}

// Register adds or replaces a Tool under the given name.
// The name convention is "server.method" but any unique string is valid.
func (r *Registry) Register(name string, tool Tool) {
	r.RegisterWithDesc(name, "", tool)
}

// RegisterWithDesc adds or replaces a Tool with an optional description.
func (r *Registry) RegisterWithDesc(name, description string, tool Tool) {
	spec := mcp.NewTool(name)
	spec.Description = description
	r.RegisterWithSpec(spec, tool)
}

// RegisterWithSpec adds or replaces a Tool with an MCP-style spec.
func (r *Registry) RegisterWithSpec(spec mcp.Tool, tool Tool) {
	r.RegisterWithSpecAndExamples(spec, nil, tool)
}

// RegisterWithSpecAndExamples adds or replaces a Tool with an MCP-style spec
// and optional example usage strings.
func (r *Registry) RegisterWithSpecAndExamples(spec mcp.Tool, examples []string, tool Tool) {
	if spec.Name == "" {
		panic("registry: tool spec name is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.tools[spec.Name] = tool
	if spec.Description != "" {
		r.descriptions[spec.Name] = spec.Description
	} else {
		delete(r.descriptions, spec.Name)
	}
	r.specs[spec.Name] = spec
	if len(examples) > 0 {
		r.examples[spec.Name] = append([]string(nil), examples...)
	} else {
		delete(r.examples, spec.Name)
	}
	r.log.Debug().Str("tool", spec.Name).Msg("tool registered")
}

// Get returns the Tool registered under name, or false if not found.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Call invokes the named Tool with the provided arguments.
func (r *Registry) Call(ctx context.Context, name string, args map[string]any) (any, error) {
	tool, ok := r.Get(name)
	if !ok {
		return nil, errors.Newf("tool %q not found in registry", name)
	}
	result, err := tool(ctx, args)
	if err != nil {
		return nil, err
	}
	return NormalizeToolResult(result), nil
}

// Names returns a sorted list of all registered tool names.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Tools returns a sorted list of ToolInfo for all registered tools.
func (r *Registry) Tools() []ToolInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	infos := make([]ToolInfo, 0, len(r.tools))
	for name := range r.tools {
		spec, ok := r.specs[name]
		if !ok {
			spec = mcp.NewTool(name)
			spec.Description = r.descriptions[name]
		}
		infos = append(infos, ToolInfo{
			Name:        name,
			Description: spec.Description,
			Spec:        spec,
			Examples:    append([]string(nil), r.examples[name]...),
		})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos
}

// Tool returns the full metadata for a registered tool.
func (r *Registry) Tool(name string) (ToolInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if _, ok := r.tools[name]; !ok {
		return ToolInfo{}, false
	}

	spec, ok := r.specs[name]
	if !ok {
		spec = mcp.NewTool(name)
		spec.Description = r.descriptions[name]
	}

	return ToolInfo{
		Name:        name,
		Description: spec.Description,
		Spec:        spec,
		Examples:    append([]string(nil), r.examples[name]...),
	}, true
}

// Has reports whether a tool with the given name is registered.
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.tools[name]
	return ok
}

// StoreCapabilities stores the full capability set discovered from a server.
func (r *Registry) StoreCapabilities(caps *ServerCapabilities) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.capabilities[caps.Name] = caps
}

// GetCapabilities returns the capabilities for a named server, or nil if unknown.
func (r *Registry) GetCapabilities(serverName string) *ServerCapabilities {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.capabilities[serverName]
}

// AllCapabilities returns capabilities for all known servers, sorted by name.
func (r *Registry) AllCapabilities() []*ServerCapabilities {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*ServerCapabilities, 0, len(r.capabilities))
	for _, c := range r.capabilities {
		result = append(result, c)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// HasServer reports whether a server alias is reserved or active.
func (r *Registry) HasServer(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.serverNames[name]
	return ok
}

// AddServer registers an MCPServer and starts managing it. The server's tools
// are automatically registered in the registry under "name.toolname" when the
// server connects.
func (r *Registry) AddServer(srv *MCPServer) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if srv == nil {
		return errors.New("registry: nil MCP server")
	}
	if _, exists := r.serverNames[srv.name]; exists {
		return errors.Newf("server %q already registered", srv.name)
	}
	r.serverNames[srv.name] = struct{}{}
	r.servers = append(r.servers, srv)
	return nil
}

// Start launches all registered MCP servers and blocks until ctx is cancelled,
// then shuts them all down.
func (r *Registry) Start(ctx context.Context) error {
	servers := r.serversSnapshot()
	if err := r.startServers(ctx, servers); err != nil {
		return err
	}

	<-ctx.Done()
	r.stopServers(servers)
	return nil
}

// StartServers launches all registered MCP servers and returns after startup
// and capability discovery complete.
func (r *Registry) StartServers(ctx context.Context) error {
	return r.startServers(ctx, r.serversSnapshot())
}

// StopServers terminates all configured MCP servers managed by the registry.
func (r *Registry) StopServers() {
	r.stopServers(r.serversSnapshot())
}

// NormalizeToolResult converts JSON object/array strings returned by tools into
// structured Go values so callers receive consistent data shapes.
func NormalizeToolResult(result any) any {
	text, ok := result.(string)
	if !ok {
		return result
	}

	trimmed := strings.TrimSpace(text)
	switch {
	case strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}"):
		var obj map[string]any
		if err := json.Unmarshal([]byte(trimmed), &obj); err == nil {
			return obj
		}
	case strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]"):
		var arr []any
		if err := json.Unmarshal([]byte(trimmed), &arr); err == nil {
			return arr
		}
	}

	return result
}

func (r *Registry) serversSnapshot() []*MCPServer {
	r.mu.RLock()
	servers := make([]*MCPServer, len(r.servers))
	copy(servers, r.servers)
	r.mu.RUnlock()
	return servers
}

func (r *Registry) startServers(ctx context.Context, servers []*MCPServer) error {
	started := 0
	for _, srv := range servers {
		if err := srv.Start(ctx, r); err != nil {
			r.log.Error().Err(err).Str("mcp_server", srv.name).Msg("failed to start MCP server")
			continue
		}
		started++
	}
	if len(servers) > 0 && started == 0 {
		r.log.Warn().
			Int("configured_servers", len(servers)).
			Msg("no MCP servers started successfully")
	}
	return nil
}

func (r *Registry) stopServers(servers []*MCPServer) {
	for _, srv := range servers {
		srv.Stop()
	}
}

// RegisterConnectedClient adds a connected MCP client to the registry under the
// provided server name.
func (r *Registry) RegisterConnectedClient(
	serverName string,
	mcpClient *client.Client,
	caps *ServerCapabilities,
	closeFn func() error,
) error {
	if caps == nil {
		return errors.New("registry: capabilities are required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.serverNames[serverName]; !exists {
		r.serverNames[serverName] = struct{}{}
	}
	if closeFn != nil {
		r.dynamic[serverName] = dynamicServer{close: closeFn}
	}

	mcpClient.OnNotification(func(notification mcp.JSONRPCNotification) {
		r.mu.RLock()
		handlers := append([]NotificationHandler(nil), r.notifications...)
		r.mu.RUnlock()
		for _, handler := range handlers {
			handler(serverName, notification.Method, notification.Params)
		}
	})

	if err := r.registerMCPToolsLocked(serverName, mcpClient, caps.Tools); err != nil {
		if closeFn != nil {
			delete(r.dynamic, serverName)
		}
		if _, hadCaps := r.capabilities[serverName]; !hadCaps {
			delete(r.serverNames, serverName)
		}
		return err
	}
	r.capabilities[serverName] = caps
	return nil
}

// UnregisterDynamicServer detaches an active dynamic MCP server and removes all
// of its registered capabilities.
func (r *Registry) UnregisterDynamicServer(name string) error {
	srv, ok := r.removeDynamicServer(name)
	if !ok {
		return errors.Newf("dynamic MCP server %q not found", name)
	}
	if srv.close == nil {
		return nil
	}
	if err := srv.close(); err != nil {
		return errors.Wrapf(err, "close dynamic MCP server %q", name)
	}
	return nil
}

// CleanupDynamicServer removes a dynamic MCP server after the underlying
// transport disconnects. Missing servers are ignored.
func (r *Registry) CleanupDynamicServer(name string) {
	r.removeDynamicServer(name)
}

// DynamicServerNames returns the active dynamic MCP server aliases.
func (r *Registry) DynamicServerNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.dynamic))
	for name := range r.dynamic {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ActiveServerCount returns the number of currently connected MCP servers.
func (r *Registry) ActiveServerCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.capabilities)
}

func (r *Registry) removeDynamicServer(name string) (dynamicServer, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	srv, ok := r.dynamic[name]
	if !ok {
		return dynamicServer{}, false
	}
	delete(r.dynamic, name)
	delete(r.serverNames, name)
	delete(r.capabilities, name)

	prefix := name + "."
	for toolName := range r.tools {
		if strings.HasPrefix(toolName, prefix) {
			delete(r.tools, toolName)
			delete(r.descriptions, toolName)
			delete(r.specs, toolName)
			delete(r.examples, toolName)
		}
	}

	return srv, true
}

func (r *Registry) registerMCPToolsLocked(
	serverName string,
	mcpClient *client.Client,
	tools []mcp.Tool,
) error {
	for _, tool := range tools {
		fqName := serverName + "." + tool.Name
		if _, exists := r.tools[fqName]; exists {
			return errors.Newf("tool %q already registered", fqName)
		}
	}

	for _, tool := range tools {
		spec := tool
		spec.Name = serverName + "." + tool.Name
		mcpToolName := tool.Name
		r.tools[spec.Name] = func(ctx context.Context, args map[string]any) (any, error) {
			req := mcp.CallToolRequest{}
			req.Params.Name = mcpToolName
			req.Params.Arguments = args
			res, err := mcpClient.CallTool(ctx, req)
			if err != nil {
				return nil, errors.Wrapf(err, "call MCP tool %q", mcpToolName)
			}
			return normalizeCallToolResult(mcpToolName, res)
		}
		if spec.Description != "" {
			r.descriptions[spec.Name] = spec.Description
		} else {
			delete(r.descriptions, spec.Name)
		}
		r.specs[spec.Name] = spec
		delete(r.examples, spec.Name)
	}

	r.log.Debug().Str("server", serverName).Int("count", len(tools)).Msg("MCP tools registered")
	return nil
}

func normalizeCallToolResult(toolName string, res *mcp.CallToolResult) (any, error) {
	if res == nil {
		return nil, nil
	}
	if res.IsError {
		return nil, errors.Newf("%s: %s", toolName, extractCallToolText(res))
	}
	if res.StructuredContent != nil {
		return res.StructuredContent, nil
	}

	var result any
	if len(res.Content) == 1 {
		if text, ok := res.Content[0].(mcp.TextContent); ok {
			result = text.Text
		}
	}
	if result == nil {
		result = extractCallToolText(res)
	}

	// Transparent JSON Promotion: If the result is a string, try to parse it as JSON.
	if s, ok := result.(string); ok && s != "" {
		trimmed := strings.TrimSpace(s)
		if (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
			(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) {
			var parsed any
			// Note: We ignore the error and fall back to the raw string if parsing fails.
			if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
				return parsed, nil
			}
		}
	}

	return result, nil
}

func extractCallToolText(res *mcp.CallToolResult) string {
	parts := make([]string, 0, len(res.Content))
	for _, item := range res.Content {
		if text, ok := item.(mcp.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}
