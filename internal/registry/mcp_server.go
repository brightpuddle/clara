// Package registry: MCPServer manages the lifecycle of a single stdio-based
// MCP server subprocess and registers its tools in the Registry.
package registry

import (
	"context"
	"fmt"
	"os"

	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"
)

// MCPServer manages a single stdio-based MCP server subprocess.
type MCPServer struct {
	name    string
	command string
	args    []string
	env     []string // KEY=VALUE pairs injected into the subprocess
	log     zerolog.Logger

	mcpClient *client.Client
}

// NewMCPServer creates an MCPServer descriptor. Call Start to launch it.
func NewMCPServer(name, command string, args []string, env map[string]string, log zerolog.Logger) *MCPServer {
	envPairs := os.Environ()
	for k, v := range env {
		envPairs = append(envPairs, fmt.Sprintf("%s=%s", k, v))
	}
	return &MCPServer{
		name:    name,
		command: command,
		args:    args,
		env:     envPairs,
		log:     log.With().Str("mcp_server", name).Logger(),
	}
}

// Start launches the subprocess, negotiates the MCP handshake, discovers
// available tools, resources, and prompts, then registers tools in r.
func (s *MCPServer) Start(ctx context.Context, r *Registry) error {
	c, err := client.NewStdioMCPClient(s.command, s.env, s.args...)
	if err != nil {
		return errors.Wrap(err, "create stdio MCP client")
	}
	s.mcpClient = c

	if err := c.Start(ctx); err != nil {
		return errors.Wrap(err, "start MCP subprocess")
	}

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    "clara",
		Version: "0.1.0",
	}
	if _, err := c.Initialize(ctx, initReq); err != nil {
		return errors.Wrap(err, "MCP initialize handshake")
	}

	caps, err := s.discoverCapabilities(ctx)
	if err != nil {
		return errors.Wrap(err, "discover capabilities")
	}
	r.StoreCapabilities(caps)
	s.registerDiscoveredTools(r, caps.Tools)

	s.log.Info().Msg("MCP server started")
	return nil
}

// Stop terminates the MCP subprocess.
func (s *MCPServer) Stop() {
	if s.mcpClient != nil {
		s.mcpClient.Close()
		s.log.Info().Msg("MCP server stopped")
	}
}

// discoverCapabilities queries the server for all tools, resources, and prompts.
func (s *MCPServer) discoverCapabilities(ctx context.Context) (*ServerCapabilities, error) {
	caps := &ServerCapabilities{Name: s.name}

	toolsResult, err := s.mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, errors.Wrap(err, "list tools")
	}
	caps.Tools = toolsResult.Tools

	// Resources and prompts are optional capabilities; treat errors as empty.
	if res, err := s.mcpClient.ListResources(ctx, mcp.ListResourcesRequest{}); err == nil {
		caps.Resources = res.Resources
	} else {
		s.log.Debug().Err(err).Msg("server does not expose resources")
	}

	if res, err := s.mcpClient.ListPrompts(ctx, mcp.ListPromptsRequest{}); err == nil {
		caps.Prompts = res.Prompts
	} else {
		s.log.Debug().Err(err).Msg("server does not expose prompts")
	}

	return caps, nil
}

// registerDiscoveredTools registers tool call handlers for the provided tool list.
func (s *MCPServer) registerDiscoveredTools(r *Registry, tools []mcp.Tool) {
	for _, tool := range tools {
		toolName := s.name + "." + tool.Name
		mcpClient := s.mcpClient
		mcpToolName := tool.Name
		desc := tool.Description
		r.RegisterWithDesc(toolName, desc, func(ctx context.Context, args map[string]any) (any, error) {
			req := mcp.CallToolRequest{}
			req.Params.Name = mcpToolName
			req.Params.Arguments = args
			res, err := mcpClient.CallTool(ctx, req)
			if err != nil {
				return nil, errors.Wrapf(err, "call MCP tool %q", mcpToolName)
			}
			return res, nil
		})
	}
	s.log.Info().Int("count", len(tools)).Msg("tools registered from MCP server")
}
