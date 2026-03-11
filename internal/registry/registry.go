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
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"
)

// Tool is the standard callable unit in Clara. Every capability — MCP tool
// call, Swift bridge call, local SQLite query — is wrapped as a Tool.
type Tool func(ctx context.Context, args map[string]any) (any, error)

// Registry holds the set of available Tools and MCP server managers.
type Registry struct {
	mu      sync.RWMutex
	tools   map[string]Tool
	servers []*MCPServer
	log     zerolog.Logger
}

// New creates an empty Registry.
func New(log zerolog.Logger) *Registry {
	return &Registry{
		tools: make(map[string]Tool),
		log:   log,
	}
}

// Register adds or replaces a Tool under the given name.
// The name convention is "server.method" but any unique string is valid.
func (r *Registry) Register(name string, tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[name] = tool
	r.log.Debug().Str("tool", name).Msg("tool registered")
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
	return names
}

// Has reports whether a tool with the given name is registered.
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.tools[name]
	return ok
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
