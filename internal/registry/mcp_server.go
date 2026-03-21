// Package registry: MCPServer manages the lifecycle of a single MCP server
// connection — either a stdio subprocess or a streamable HTTP endpoint.
package registry

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"
)

// httpReconnectInterval is how often background reconnect attempts are made
// for HTTP MCP servers that were not reachable at startup.
const httpReconnectInterval = 30 * time.Second

// MCPServer manages a single MCP server connection — either a stdio subprocess
// or a streamable HTTP endpoint.
type MCPServer struct {
	name        string
	description string
	url         string // non-empty for HTTP servers
	command     string
	args        []string
	env         []string // KEY=VALUE pairs injected into the subprocess
	searchPaths []string
	log         zerolog.Logger

	mcpClient *client.Client
	startFn   func(ctx context.Context, r *Registry) error
	stopFn    func()

	// httpConnected tracks whether the HTTP MCP server is currently connected.
	// Accessed atomically so the background reconnect goroutine can check it
	// without holding any lock.
	httpConnected atomic.Bool
}

// NewMCPServer creates an MCPServer descriptor for a stdio subprocess. Call
// Start to launch it.
func NewMCPServer(
	name, description, command string,
	args []string,
	env map[string]string,
	searchPaths []string,
	log zerolog.Logger,
) *MCPServer {
	return &MCPServer{
		name:        name,
		description: description,
		command:     command,
		args:        args,
		env:         buildServerEnv(env, searchPaths),
		searchPaths: append([]string(nil), searchPaths...),
		log:         log.With().Str("mcp_server", name).Logger(),
	}
}

// NewHTTPMCPServer creates an MCPServer descriptor for a streamable HTTP MCP
// endpoint. Call Start to connect to it.
func NewHTTPMCPServer(
	name, description, url string,
	log zerolog.Logger,
) *MCPServer {
	return &MCPServer{
		name:        name,
		description: description,
		url:         url,
		log:         log.With().Str("mcp_server", name).Logger(),
	}
}

// Start connects to the MCP server (HTTP or stdio), negotiates the MCP
// handshake, discovers available tools, resources, and prompts, then registers
// tools in r.
func (s *MCPServer) Start(ctx context.Context, r *Registry) error {
	if s.startFn != nil {
		return s.startFn(ctx, r)
	}
	if s.url != "" {
		return s.startHTTP(ctx, r)
	}
	return s.startStdio(ctx, r)
}

// startHTTP attempts to connect to the streamable HTTP MCP endpoint. If the
// server is not reachable, the error is logged as a warning and a background
// goroutine is started that retries the connection every 30 seconds. The daemon
// continues starting normally — HTTP server unavailability at startup is not
// fatal, since servers like mcp-chrome-bridge are managed by the browser and
// come up whenever their host application (e.g. Chrome) is running.
func (s *MCPServer) startHTTP(ctx context.Context, r *Registry) error {
	if err := s.connectHTTP(ctx, r); err != nil {
		s.log.Warn().
			Err(err).
			Str("url", s.url).
			Dur("retry_interval", httpReconnectInterval).
			Msg("HTTP MCP server not reachable at startup; retrying in background")
		go s.backgroundReconnect(ctx, r)
		return nil // non-fatal: daemon starts without this server's tools for now
	}
	return nil
}

// connectHTTP performs a single attempt to connect to the HTTP MCP server,
// negotiate the handshake, and register its tools. It is a no-op if the server
// is already connected.
func (s *MCPServer) connectHTTP(ctx context.Context, r *Registry) error {
	if s.httpConnected.Load() {
		return nil
	}

	c, err := client.NewStreamableHttpClient(s.url)
	if err != nil {
		return errors.Wrap(err, "create streamable HTTP MCP client")
	}

	if err := c.Start(ctx); err != nil {
		return errors.Wrapf(err, "connect to HTTP MCP server %s", s.url)
	}

	caps, err := initializeConnectedClient(ctx, s.name, c)
	if err != nil {
		return err
	}
	caps.Description = preferredServiceDescription(s.description, caps.Description)
	if err := r.RegisterConnectedClient(s.name, c, caps, nil); err != nil {
		return err
	}

	s.mcpClient = c
	s.httpConnected.Store(true)
	s.log.Info().Str("url", s.url).Msg("HTTP MCP server connected")
	return nil
}

// backgroundReconnect retries connecting to the HTTP MCP server at a fixed
// interval until either the connection succeeds or the context is cancelled.
func (s *MCPServer) backgroundReconnect(ctx context.Context, r *Registry) {
	ticker := time.NewTicker(httpReconnectInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.httpConnected.Load() {
				return
			}
			if err := s.connectHTTP(ctx, r); err != nil {
				s.log.Debug().
					Str("url", s.url).
					Msg("HTTP MCP server still unavailable; will retry")
				continue
			}
			return
		}
	}
}

// startStdio launches a subprocess MCP server and connects via stdio.
func (s *MCPServer) startStdio(ctx context.Context, r *Registry) error {
	command, err := resolveMCPCommand(s.command, s.searchPaths)
	if err != nil {
		return err
	}

	c, err := client.NewStdioMCPClient(command, s.env, s.args...)
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

// Stop terminates the MCP server connection.
func (s *MCPServer) Stop() {
	if s.stopFn != nil {
		s.stopFn()
		return
	}
	if s.mcpClient != nil {
		s.mcpClient.Close()
		s.log.Info().Msg("MCP server stopped")
	}
}

func buildServerEnv(env map[string]string, searchPaths []string) []string {
	envMap := make(map[string]string, len(os.Environ())+len(env)+1)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		envMap[key] = value
	}

	pathEntries := make([]string, 0, len(searchPaths)+8)
	pathEntries = append(pathEntries, searchPaths...)
	if existingPath, ok := env["PATH"]; ok {
		pathEntries = append(pathEntries, filepath.SplitList(existingPath)...)
	} else {
		pathEntries = append(pathEntries, filepath.SplitList(envMap["PATH"])...)
	}
	envMap["PATH"] = strings.Join(dedupeSearchPaths(pathEntries), string(os.PathListSeparator))

	for k, v := range env {
		if k == "PATH" {
			continue
		}
		envMap[k] = v
	}

	envPairs := make([]string, 0, len(envMap))
	for key, value := range envMap {
		envPairs = append(envPairs, fmt.Sprintf("%s=%s", key, value))
	}
	return envPairs
}

func resolveMCPCommand(command string, searchPaths []string) (string, error) {
	if strings.Contains(command, string(os.PathSeparator)) {
		return command, nil
	}

	for _, dir := range dedupeSearchPaths(searchPaths) {
		candidate := filepath.Join(dir, command)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Mode()&0o111 == 0 {
			continue
		}
		return candidate, nil
	}

	resolved, err := exec.LookPath(command)
	if err == nil {
		return resolved, nil
	}
	return "", errors.Wrapf(err, "resolve MCP command %q", command)
}

func dedupeSearchPaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	deduped := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		deduped = append(deduped, path)
	}
	return deduped
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
