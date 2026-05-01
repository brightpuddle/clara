// Package registry: MCP server management methods for Registry.
package registry

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

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

// dynamicServer holds the close function for a dynamically attached MCP server.
type dynamicServer struct {
	close func() error
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
// Startup handshakes continue in the background. Use WaitReady to block until
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

	// Start the watchdog to monitor server health.
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

		pCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, err := s.mcpClient.ListTools(pCtx, mcp.ListToolsRequest{})
		cancel()

		if err != nil {
			r.log.Warn().
				Err(err).
				Str("mcp_server", s.name).
				Msg("MCP server unresponsive; restarting")
			go func(srv *MCPServer) {
				restartCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				if err := r.RestartServer(restartCtx, srv.name); err != nil {
					r.log.Error().
						Err(err).
						Str("mcp_server", srv.name).
						Msg("watchdog failed to restart server")
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

// RestartServer stops and then starts a named MCP server.
func (r *Registry) RestartServer(ctx context.Context, name string) error {
	if err := r.StopServer(name); err != nil {
		return err
	}
	return r.StartServer(ctx, name)
}

// HasServer reports whether a server alias is reserved or active.
func (r *Registry) HasServer(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.serverNames[name]
	return ok
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

// RegisterConnectedClient adds a connected MCP client to the registry under
// the provided server name.
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

func (r *Registry) registerMCPToolsLocked(
	serverName string,
	mcpClient *client.Client,
	tools []mcp.Tool,
) error {
	for _, tool := range tools {
		nsName := r.GetFQToolName(serverName, tool.Name)
		if _, exists := r.tools[nsName]; exists {
			return errors.Newf("tool %q already registered", nsName)
		}
	}

	for _, tool := range tools {
		nsName := r.GetFQToolName(serverName, tool.Name)
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

		spec := tool
		spec.Name = nsName
		r.addToolLocked(nsName, spec, toolFn)
		r.serverTools[serverName] = append(r.serverTools[serverName], nsName)
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

func (r *Registry) serversSnapshot() []*MCPServer {
	r.mu.RLock()
	servers := make([]*MCPServer, len(r.servers))
	copy(servers, r.servers)
	r.mu.RUnlock()
	return servers
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
	return NormalizeToolResult(result), nil
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
