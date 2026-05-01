// Package registry manages the Clara tool registry. It is the central hub that
// holds all Tool implementations.
//
// Tools are registered by name and invoked by the interpreter. The naming
// convention is typically "namespace.method" (e.g. "fs.read_file").
package registry

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"
)

// Tool is the standard callable unit in Clara. Every capability — plugin tool
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

// Registry holds the set of available Tools.
type Registry struct {
	mu                    sync.RWMutex
	tools                 map[string]Tool
	defaultTools          map[string]Tool
	descriptions          map[string]string
	namespaceDescriptions map[string]string
	specs                 map[string]mcp.Tool
	examples              map[string][]string
	notifications         []NotificationHandler
	log                   zerolog.Logger
}

// New creates an empty Registry.
func New(log zerolog.Logger) *Registry {
	return &Registry{
		tools:                 make(map[string]Tool),
		defaultTools:          make(map[string]Tool),
		descriptions:          make(map[string]string),
		namespaceDescriptions: make(map[string]string),
		specs:                 make(map[string]mcp.Tool),
		examples:              make(map[string][]string),
		log:                   log,
	}
}

// Subscribe registers a callback for notifications.
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

// Namespaces returns a sorted list of all active namespaces.
func (r *Registry) Namespaces() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := make(map[string]struct{})
	// Include namespaces from tool names (e.g. "mail" from "mail.search")
	for name := range r.tools {
		if dot := strings.Index(name, "."); dot != -1 {
			seen[name[:dot]] = struct{}{}
		}
	}
	for name := range r.defaultTools {
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
// or a prefix derived from registered tools.
func (r *Registry) IsKnownNamespace(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

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

func (r *Registry) deleteToolLocked(name string) {
	delete(r.tools, name)
	delete(r.descriptions, name)
	delete(r.specs, name)
	delete(r.examples, name)
}

// UnregisterNamespace removes all tools belonging to a namespace prefix
// (e.g., "shell.") and clears namespace descriptions associated with that namespace.
func (r *Registry) UnregisterNamespace(ns string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.namespaceDescriptions, ns)

	prefix := ns + "."
	for toolName := range r.tools {
		if strings.HasPrefix(toolName, prefix) {
			r.deleteToolLocked(toolName)
		}
	}
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

// NamespaceDescription returns the human-readable summary for a mapped namespace.
func (r *Registry) NamespaceDescription(name string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.namespaceDescriptions[name]
}

func (r *Registry) GetFQToolName(serverName, toolName string) string {
	if strings.HasPrefix(toolName, serverName+".") {
		return toolName
	}
	return serverName + "." + toolName
}
