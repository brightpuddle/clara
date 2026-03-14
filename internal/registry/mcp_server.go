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
	name        string
	description string
	command     string
	args        []string
	env         []string // KEY=VALUE pairs injected into the subprocess
	log         zerolog.Logger

	mcpClient *client.Client
}

// NewMCPServer creates an MCPServer descriptor. Call Start to launch it.
func NewMCPServer(
	name, description, command string,
	args []string,
	env map[string]string,
	log zerolog.Logger,
) *MCPServer {
	envPairs := os.Environ()
	for k, v := range env {
		envPairs = append(envPairs, fmt.Sprintf("%s=%s", k, v))
	}
	return &MCPServer{
		name:        name,
		description: description,
		command:     command,
		args:        args,
		env:         envPairs,
		log:         log.With().Str("mcp_server", name).Logger(),
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

	caps, err := initializeConnectedClient(ctx, s.name, c)
	if err != nil {
		return err
	}
	caps.Description = preferredServiceDescription(s.description, caps.Description)
	if err := r.RegisterConnectedClient(s.name, c, caps, nil); err != nil {
		return err
	}

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

func initializeConnectedClient(
	ctx context.Context,
	name string,
	mcpClient *client.Client,
) (*ServerCapabilities, error) {
	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    "clara",
		Version: "0.1.0",
	}
	initResult, err := mcpClient.Initialize(ctx, initReq)
	if err != nil {
		return nil, errors.Wrap(err, "MCP initialize handshake")
	}

	caps := &ServerCapabilities{
		Name:        name,
		Description: preferredServiceDescription("", initResult.ServerInfo.Description),
	}

	toolsResult, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, errors.Wrap(err, "list tools")
	}
	caps.Tools = toolsResult.Tools

	// Resources and prompts are optional capabilities; treat errors as empty.
	if res, err := mcpClient.ListResources(ctx, mcp.ListResourcesRequest{}); err == nil {
		caps.Resources = res.Resources
	}

	if res, err := mcpClient.ListPrompts(ctx, mcp.ListPromptsRequest{}); err == nil {
		caps.Prompts = res.Prompts
	}

	return caps, nil
}

func preferredServiceDescription(configDescription, discoveredDescription string) string {
	if configDescription != "" {
		return configDescription
	}
	return discoveredDescription
}
