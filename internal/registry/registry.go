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
	"sort"
	"sync"

	"github.com/cockroachdb/errors"
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
	mu           sync.RWMutex
	tools        map[string]Tool
	descriptions map[string]string
	specs        map[string]mcp.Tool
	examples     map[string][]string
	servers      []*MCPServer
	capabilities map[string]*ServerCapabilities
	log          zerolog.Logger
}

// New creates an empty Registry.
func New(log zerolog.Logger) *Registry {
	return &Registry{
		tools:        make(map[string]Tool),
		descriptions: make(map[string]string),
		specs:        make(map[string]mcp.Tool),
		examples:     make(map[string][]string),
		capabilities: make(map[string]*ServerCapabilities),
		log:          log,
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
	return tool(ctx, args)
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

// AddServer registers an MCPServer and starts managing it. The server's tools
// are automatically registered in the registry under "name.toolname" when the
// server connects.
func (r *Registry) AddServer(srv *MCPServer) {
	r.mu.Lock()
	r.servers = append(r.servers, srv)
	r.mu.Unlock()
}

// Start launches all registered MCP servers and blocks until ctx is cancelled,
// then shuts them all down.
func (r *Registry) Start(ctx context.Context) error {
	r.mu.RLock()
	servers := make([]*MCPServer, len(r.servers))
	copy(servers, r.servers)
	r.mu.RUnlock()

	for _, srv := range servers {
		if err := srv.Start(ctx, r); err != nil {
			return errors.Wrapf(err, "start MCP server %q", srv.name)
		}
	}

	<-ctx.Done()

	for _, srv := range servers {
		srv.Stop()
	}
	return nil
}
