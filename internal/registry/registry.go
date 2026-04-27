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
	"time"

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

var namespaceDefaults = []struct {
	prefix      string
	namespace   string
	description string
}{
	{"reminders_", "reminders", "Apple Reminders"},
	{"theme_", "theme", "System Theme"},
	{"photos_", "photos", "Apple Photos"},
	{"notify_", "notify", "MacOS Notifications"},
	{"mail_", "mail", "Apple Mail"},
	{"events_", "calendar", "Apple Calendar"},
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
	tools          map[string]Tool
	defaultTools   map[string]Tool
	descriptions  map[string]string
	namespaceDescriptions map[string]string
	specs         map[string]mcp.Tool
	examples      map[string][]string
	servers       []*MCPServer
	serverNames   map[string]struct{}
	dynamic       map[string]dynamicServer
	capabilities  map[string]*ServerCapabilities
	serverTools   map[string][]string // serverName -> []toolNames
	notifications []NotificationHandler
	log           zerolog.Logger

	pendingServers map[string]struct{}
	readyChan      chan struct{}

	watchdogCancel context.CancelFunc
}

type dynamicServer struct {
	close func() error
}

// New creates an empty Registry.
func New(log zerolog.Logger) *Registry {
	readyChan := make(chan struct{})
	close(readyChan) // Ready by default until StartServers is called with servers.

	return &Registry{
		tools:          make(map[string]Tool),
		defaultTools:   make(map[string]Tool),
		descriptions:   make(map[string]string),
		namespaceDescriptions: make(map[string]string),
		specs:          make(map[string]mcp.Tool),
		examples:       make(map[string][]string),
		serverNames:    make(map[string]struct{}),
		dynamic:        make(map[string]dynamicServer),
		capabilities:   make(map[string]*ServerCapabilities),
		serverTools:    make(map[string][]string),
		pendingServers: make(map[string]struct{}),
		readyChan:      readyChan,
		log:            log,
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

// RegisterNamespaceDescription registers a human-readable summary for a namespace.
func (r *Registry) RegisterNamespaceDescription(name, description string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.namespaceDescriptions[name] = description
}

// RegisterDefault adds or replaces a Tool in the default/fallback set.
func (r *Registry) RegisterDefault(name string, tool Tool) {
	spec := mcp.NewTool(name)
	r.RegisterDefaultWithSpec(spec, tool)
}

// RegisterDefaultWithSpec adds or replaces a Tool in the default/fallback set.
func (r *Registry) RegisterDefaultWithSpec(spec mcp.Tool, tool Tool) {
	if spec.Name == "" {
		panic("registry: tool spec name is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.defaultTools[spec.Name] = tool
	if spec.Description != "" {
		r.descriptions[spec.Name] = spec.Description
	}
	r.specs[spec.Name] = spec
	r.log.Debug().Str("default_tool", spec.Name).Msg("default tool registered")
}

// Get returns the Tool registered under name, or false if not found.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	if !ok {
		t, ok = r.defaultTools[name]
	}
	return t, ok
}

// Call invokes the named Tool with the provided arguments.
func (r *Registry) Call(ctx context.Context, name string, args map[string]any) (any, error) {
	tool, ok := r.Get(name)
	if !ok {
		return nil, errors.Newf("tool %q not found in registry", name)
	}

	// Apply a default timeout of 30 seconds if one isn't already set.
	// This prevents a hanging MCP server from blocking the CLI or a task forever.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
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
	seen := make(map[string]struct{}, len(r.tools)+len(r.defaultTools))
	for name := range r.tools {
		seen[name] = struct{}{}
	}
	for name := range r.defaultTools {
		seen[name] = struct{}{}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Tools returns a sorted list of ToolInfo for all registered tools.
func (r *Registry) Tools() []ToolInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	seen := make(map[string]struct{}, len(r.tools)+len(r.defaultTools))
	for name := range r.tools {
		seen[name] = struct{}{}
	}
	for name := range r.defaultTools {
		seen[name] = struct{}{}
	}

	infos := make([]ToolInfo, 0, len(seen))
	for name := range seen {
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
	if !ok {
		_, ok = r.defaultTools[name]
	}
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

// Namespaces returns a sorted list of all active namespaces (both server names
// and mapped sub-namespaces).
func (r *Registry) Namespaces() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := make(map[string]struct{})
	for _, d := range namespaceDefaults {
		seen[d.namespace] = struct{}{}
	}
	for name := range r.capabilities {
		seen[name] = struct{}{}
	}
	// Also include server names that are reserved but not yet connected
	for name := range r.serverNames {
		seen[name] = struct{}{}
	}
	// Include namespaces from tool names (e.g. "mail" from "mail.search")
	for name := range r.tools {
		if dot := strings.Index(name, "."); dot != -1 {
			seen[name[:dot]] = struct{}{}
		}
	}

	result := make([]string, 0, len(seen))
	for ns := range seen {
		result = append(result, ns)
	}
	sort.Strings(result)
	return result
}

// IsKnownNamespace reports whether a namespace string is a registered server name,
// a hardcoded default namespace, or a prefix derived from registered tools.
func (r *Registry) IsKnownNamespace(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if name == "tui" {
		return true
	}
	for _, d := range namespaceDefaults {
		if d.namespace == name {
			return true
		}
	}
	if _, ok := r.serverNames[name]; ok {
		return true
	}
	if _, ok := r.capabilities[name]; ok {
		return true
	}
	prefix := name + "."
	for t := range r.tools {
		if strings.HasPrefix(t, prefix) {
			return true
		}
	}
	for t := range r.defaultTools {
		if strings.HasPrefix(t, prefix) {
			return true
		}
	}
	return false
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

// StartServers launches all registered MCP servers and returns immediately.
// Startup Handshakes continue in the background. Use WaitReady to block until
// all servers have either connected or failed.
func (r *Registry) StartServers(ctx context.Context) error {
	servers := r.serversSnapshot()
	if len(servers) == 0 {
		return nil
	}

	r.mu.Lock()
	r.pendingServers = make(map[string]struct{})
	for _, s := range servers {
		r.pendingServers[s.name] = struct{}{}
	}
	r.readyChan = make(chan struct{})
	r.mu.Unlock()

	r.log.Info().Int("count", len(servers)).Msg("starting MCP servers in background")
	for _, srv := range servers {
		go func(s *MCPServer) {
			err := s.Start(ctx, r)
			if err != nil {
				r.log.Error().Err(err).Str("mcp_server", s.name).Msg("failed to start MCP server")
			}
			r.markServerDone(s.name)
		}(srv)
	}

	// Start the watchdog to monitor server health
	r.mu.Lock()
	if r.watchdogCancel != nil {
		r.watchdogCancel()
	}
	wCtx, cancel := context.WithCancel(context.Background())
	r.watchdogCancel = cancel
	r.mu.Unlock()
	go r.runWatchdog(wCtx)

	return nil
}

func (r *Registry) runWatchdog(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.checkServerHealth(ctx)
		}
	}
}

func (r *Registry) checkServerHealth(ctx context.Context) {
	servers := r.serversSnapshot()
	for _, s := range servers {
		if s.Status() != StatusRunning {
			continue
		}

		// Ping the server with a short timeout. ListTools is a lightweight way to check if it's alive.
		pCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, err := s.mcpClient.ListTools(pCtx, mcp.ListToolsRequest{})
		cancel()

		if err != nil {
			r.log.Warn().Err(err).Str("mcp_server", s.name).Msg("MCP server unresponsive; restarting")
			go func(srv *MCPServer) {
				// Use a fresh context for restart
				restartCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				if err := r.RestartServer(restartCtx, srv.name); err != nil {
					r.log.Error().Err(err).Str("mcp_server", srv.name).Msg("watchdog failed to restart server")
				}
			}(s)
		}
	}
}

// WaitReady blocks until all servers from the initial StartServers call have
// either connected successfully or failed to start, or until ctx is cancelled.
func (r *Registry) WaitReady(ctx context.Context) error {
	r.mu.RLock()
	ready := r.readyChan
	r.mu.RUnlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ready:
		return nil
	}
}

func (r *Registry) markServerDone(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.pendingServers[name]; !ok {
		return
	}
	delete(r.pendingServers, name)
	if len(r.pendingServers) == 0 {
		close(r.readyChan)
	}
}

// StopServers terminates all configured MCP servers managed by the registry.
func (r *Registry) StopServers() {
	servers := r.serversSnapshot()
	for _, srv := range servers {
		_ = r.StopServer(srv.name)
	}
}

// StartServer manually launches a single named MCP server.
func (r *Registry) StartServer(ctx context.Context, name string) error {
	r.mu.RLock()
	var srv *MCPServer
	for _, s := range r.servers {
		if s.name == name {
			srv = s
			break
		}
	}
	r.mu.RUnlock()

	if srv == nil {
		return errors.Newf("server %q not found", name)
	}

	return srv.Start(ctx, r)
}

func (r *Registry) deleteToolLocked(name string) {
	delete(r.tools, name)
	delete(r.descriptions, name)
	delete(r.specs, name)
	delete(r.examples, name)
}

// StopServer manually terminates a single named MCP server and removes its tools.
func (r *Registry) StopServer(name string) error {
	r.mu.Lock()
	var srv *MCPServer
	for _, s := range r.servers {
		if s.name == name {
			srv = s
			break
		}
	}
	if srv != nil {
		delete(r.capabilities, name)
		if tools, ok := r.serverTools[name]; ok {
			for _, toolName := range tools {
				r.deleteToolLocked(toolName)
			}
			delete(r.serverTools, name)
		}

		prefix := name + "."
		for toolName := range r.tools {
			if strings.HasPrefix(toolName, prefix) {
				r.deleteToolLocked(toolName)
			}
		}
	}
	r.mu.Unlock()

	if srv == nil {
		return errors.Newf("server %q not found", name)
	}

	srv.Stop()
	return nil
}

// RemoveServer stops and removes a server from the registry.
func (r *Registry) RemoveServer(name string) error {
	_ = r.StopServer(name)

	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.serverNames, name)
	for i, s := range r.servers {
		if s.name == name {
			r.servers = append(r.servers[:i], r.servers[i+1:]...)
			break
		}
	}
	return nil
}

// UnregisterNamespace removes all tools belonging to a namespace prefix
// (e.g., "shell.") and clears capabilities, server names, and namespace
// descriptions associated with that namespace.
func (r *Registry) UnregisterNamespace(ns string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.capabilities, ns)
	delete(r.serverNames, ns)
	delete(r.namespaceDescriptions, ns)

	if tools, ok := r.serverTools[ns]; ok {
		for _, toolName := range tools {
			r.deleteToolLocked(toolName)
		}
		delete(r.serverTools, ns)
	}

	prefix := ns + "."
	for toolName := range r.tools {
		if strings.HasPrefix(toolName, prefix) {
			r.deleteToolLocked(toolName)
		}
	}
}

// RestartServer stops and then starts a named MCP server.
func (r *Registry) RestartServer(ctx context.Context, name string) error {
	if err := r.StopServer(name); err != nil {
		return err
	}
	return r.StartServer(ctx, name)
}

// ServerStatuses returns a map of server names to their current statuses.
func (r *Registry) ServerStatuses() map[string]ServerStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	statuses := make(map[string]ServerStatus)
	for _, s := range r.servers {
		statuses[s.name] = s.Status()
	}
	return statuses
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

		method := notification.Method
		sourceServer := serverName

		// Apply namespace mapping to notifications based on hardcoded defaults.
		normalizedMethod := strings.TrimPrefix(method, "clara/")
		method = normalizedMethod

		defaults := []struct {
			prefix    string
			namespace string
		}{
			{"reminders_", "reminders"},
			{"theme_", "theme"},
			{"photos_", "photos"},
			{"notify_", "notify"},
			{"mail_", "mail"},
			{"events_", "calendar"},
		}

		for _, d := range defaults {
			if strings.HasPrefix(normalizedMethod, d.prefix) {
				sourceServer = d.namespace
				method = strings.TrimPrefix(normalizedMethod, d.prefix)
				break
			}
		}

		for _, handler := range handlers {
			handler(sourceServer, method, notification.Params)
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

// NamespaceDescription returns the human-readable summary for a mapped namespace.
func (r *Registry) NamespaceDescription(name string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if desc, ok := r.namespaceDescriptions[name]; ok {
		return desc
	}

	for _, d := range namespaceDefaults {
		if d.namespace == name {
			return d.description
		}
	}
	return ""
}

// NamespaceMeta returns the parent server name and event-name prefix for a
// sub-namespace (e.g. "reminders" → server="macos", prefix="reminders").
// ok is false when name is not a registered sub-namespace.
func (r *Registry) NamespaceMeta(ns string) (server, prefix string, ok bool) {
	for _, d := range namespaceDefaults {
		if d.namespace == ns {
			// Currently, all hardcoded namespaces are assumed to be hosted by
			// the "macos" MCP server (ClaraBridge).
			return "macos", strings.TrimSuffix(d.prefix, "_"), true
		}
	}
	return "", "", false
}

// ServerNamespacePrefixes returns a map of namespace→prefix for every
// sub-namespace that belongs to the given server. Used to determine which
// event names are "claimed" by a sub-namespace and should be hidden from the
// parent server's event listing.
func (r *Registry) ServerNamespacePrefixes(serverName string) map[string]string {
	result := make(map[string]string)
	if serverName != "macos" {
		return result
	}

	for _, d := range namespaceDefaults {
		result[d.namespace] = strings.TrimSuffix(d.prefix, "_")
	}
	return result
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

	if tools, ok := r.serverTools[name]; ok {
		for _, toolName := range tools {
			r.deleteToolLocked(toolName)
		}
		delete(r.serverTools, name)
	}

	prefix := name + "."
	for toolName := range r.tools {
		if strings.HasPrefix(toolName, prefix) {
			r.deleteToolLocked(toolName)
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
		cleanName := r.getFQToolName(serverName, tool.Name)
		nsName := cleanName
		if !strings.HasPrefix(cleanName, serverName+".") {
			nsName = serverName + "." + cleanName
		}

		if _, exists := r.tools[nsName]; exists {
			return errors.Newf("tool %q already registered", nsName)
		}
	}

	for _, tool := range tools {
		cleanName := r.getFQToolName(serverName, tool.Name)
		nsName := cleanName
		if !strings.HasPrefix(cleanName, serverName+".") {
			nsName = serverName + "." + cleanName
		}

		mcpToolName := tool.Name
		toolFn := func(ctx context.Context, args map[string]any) (any, error) {
			req := mcp.CallToolRequest{}
			req.Params.Name = mcpToolName
			req.Params.Arguments = args
			res, err := mcpClient.CallTool(ctx, req)
			if err != nil {
				return nil, errors.Wrapf(err, "call MCP tool %q", mcpToolName)
			}
			return normalizeCallToolResult(mcpToolName, res)
		}

		// Register under nsName (always succeeds).
		r.addToolLocked(nsName, tool, toolFn)
		r.serverTools[serverName] = append(r.serverTools[serverName], nsName)

		// Register under cleanName (only if not already taken).
		if cleanName != nsName {
			if _, exists := r.tools[cleanName]; !exists {
				r.addToolLocked(cleanName, tool, toolFn)
				r.serverTools[serverName] = append(r.serverTools[serverName], cleanName)
			}
		}
	}

	r.log.Debug().Str("server", serverName).Int("count", len(tools)).Msg("MCP tools registered")
	return nil
}

func (r *Registry) addToolLocked(name string, spec mcp.Tool, tool Tool) {
	spec.Name = name
	r.tools[name] = tool
	if spec.Description != "" {
		r.descriptions[name] = spec.Description
	} else {
		delete(r.descriptions, name)
	}
	r.specs[name] = spec
	delete(r.examples, name)
}

func (r *Registry) getFQToolName(serverName, toolName string) string {
	// SEP-986 Hardcoded defaults:
	// reminders_ -> reminders.
	// theme_ -> theme.
	// photos_ -> photos.
	// notify_ -> notify.
	// mail_ -> mail.
	// events_ -> calendar.
	defaults := []struct {
		prefix    string
		namespace string
	}{
		{"reminders_", "reminders"},
		{"theme_", "theme"},
		{"photos_", "photos"},
		{"notify_", "notify"},
		{"mail_", "mail"},
		{"events_", "calendar"},
	}

	for _, d := range defaults {
		if strings.HasPrefix(toolName, d.prefix) {
			return d.namespace + "." + strings.TrimPrefix(toolName, d.prefix)
		}
	}

	// SEP-986: If the tool name already contains a dot, assume it's already
	// fully qualified or namespaced correctly.
	if strings.Contains(toolName, ".") {
		return toolName
	}

	// Fallback: Treat the server name as a default prefix if it matches the tool name.
	if strings.HasPrefix(toolName, serverName+"_") {
		return serverName + "." + strings.TrimPrefix(toolName, serverName+"_")
	}

	return serverName + "." + toolName
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
