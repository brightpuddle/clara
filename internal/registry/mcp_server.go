// Package registry: MCPServer manages the lifecycle of a single MCP server
// connection — either a stdio subprocess or a streamable HTTP endpoint.
package registry

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/google/shlex"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"
)

// httpReconnectInterval is how often background reconnect attempts are made
// for HTTP MCP servers that were not reachable at startup.
const httpReconnectInterval = 30 * time.Second

type ServerStatus string

const (
	StatusStopped    ServerStatus = "STOPPED"
	StatusConnecting ServerStatus = "CONNECTING"
	StatusRunning    ServerStatus = "RUNNING"
	StatusFailed     ServerStatus = "FAILED"
)

// MCPServer manages a single MCP server connection — either a stdio subprocess
// or a streamable HTTP endpoint.
type MCPServer struct {
	name        string
	description string
	url         string // non-empty for HTTP servers
	token       string // bearer token for HTTP servers
	skipVerify  bool   // skip TLS verification for HTTP servers
	command     string
	args        []string
	env         []string // KEY=VALUE pairs injected into the subprocess
	searchPaths []string
	log         zerolog.Logger

	mu        sync.RWMutex
	status    ServerStatus
	cancel    context.CancelFunc // To stop the server
	mcpClient *client.Client
	startFn   func(ctx context.Context, r *Registry) error
	stopFn    func()

	// httpConnected tracks whether the HTTP MCP server is currently connected.
	// Accessed atomically so the background reconnect goroutine can check it
	// without holding any lock.
	httpConnected atomic.Bool
}

// Status returns the current server status in a thread-safe way.
func (s *MCPServer) Status() ServerStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *MCPServer) setStatus(status ServerStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
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
		status:      StatusStopped,
	}
}

// NewHTTPMCPServer creates an MCPServer descriptor for a streamable HTTP MCP
// endpoint. Call Start to connect to it.
func NewHTTPMCPServer(
	name, description, url, token string,
	skipVerify bool,
	log zerolog.Logger,
) *MCPServer {
	return &MCPServer{
		name:        name,
		description: description,
		url:         url,
		token:       token,
		skipVerify:  skipVerify,
		log:         log.With().Str("mcp_server", name).Logger(),
		status:      StatusStopped,
	}
}

// NewTestMCPServer creates an MCPServer for testing with custom start and stop
// logic.
func NewTestMCPServer(name string, start func(context.Context, *Registry) error, stop func()) *MCPServer {
	return &MCPServer{
		name:    name,
		startFn: start,
		stopFn:  stop,
		status:  StatusStopped,
		log:     zerolog.Nop(),
	}
}

// Start connects to the MCP server (HTTP or stdio), negotiates the MCP
// handshake, discovers available tools, resources, and prompts, then registers
// tools in r.
func (s *MCPServer) Start(ctx context.Context, r *Registry) error {
	s.setStatus(StatusConnecting)
	startCtx, cancel := context.WithCancel(ctx)

	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()

	var err error
	defer func() {
		if err != nil {
			s.setStatus(StatusFailed)
			cancel()
		} else {
			s.mu.Lock()
			if s.status != StatusConnecting {
				s.status = StatusRunning
			}
			s.mu.Unlock()
		}
	}()

	if s.startFn != nil {
		err = s.startFn(startCtx, r)
		if err == nil {
			s.setStatus(StatusRunning)
		}
		return err
	}
	if s.url != "" {
		err = s.startHTTP(startCtx, r)
		return err
	}
	err = s.startStdio(startCtx, r)
	return err
}

// startHTTP attempts to connect to the streamable HTTP MCP endpoint. If the
// server is not reachable, the error is logged as a warning and a background
// goroutine is started that retries the connection every 30 seconds. The daemon
// continues starting normally — HTTP server unavailability at startup is not
// fatal, since servers like mcp-chrome-bridge are managed by the browser and
// come up whenever their host application (e.g. Chrome) is running.
func (s *MCPServer) startHTTP(ctx context.Context, r *Registry) error {
	// Give the initial connection attempt a short timeout to prevent blocking startup.
	initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := s.connectHTTP(initCtx, r); err != nil {
		s.log.Warn().
			Err(err).
			Str("url", s.url).
			Dur("retry_interval", httpReconnectInterval).
			Msg("HTTP MCP server not reachable at startup; retrying in background")

		s.setStatus(StatusConnecting)
		go s.backgroundReconnect(ctx, r)
		return nil // Return nil so Start() doesn't cancel the context.
	}
	s.setStatus(StatusRunning)
	return nil
}

// connectHTTP performs a single attempt to connect to the HTTP MCP server,
// negotiate the handshake, and register its tools. It is a no-op if the server
// is already connected.
func (s *MCPServer) connectHTTP(ctx context.Context, r *Registry) error {
	if s.httpConnected.Load() {
		return nil
	}

	var opts []transport.StreamableHTTPCOption
	if s.token != "" {
		tokenPreview := s.token
		if len(tokenPreview) > 4 {
			tokenPreview = tokenPreview[:4] + "..."
		}
		s.log.Debug().Str("token_preview", tokenPreview).Msg("sending Bearer token in Authorization header")
		opts = append(opts, transport.WithHTTPHeaders(map[string]string{
			"Authorization": "Bearer " + s.token,
		}))
	}

	if s.skipVerify && strings.HasPrefix(strings.ToLower(s.url), "https://") {
		customClient := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}
		opts = append(opts, transport.WithHTTPBasicClient(customClient))
	}

	s.log.Debug().Str("url", s.url).Msg("attempting to connect to HTTP MCP server")
	c, err := client.NewStreamableHttpClient(s.url, opts...)
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

	s.mu.Lock()
	s.mcpClient = c
	s.mu.Unlock()

	s.httpConnected.Store(true)
	s.setStatus(StatusRunning)
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
					Err(err).
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
	args := s.args
	command := s.command
	if len(args) == 0 && command != "" {
		split, err := shlex.Split(command)
		if err == nil && len(split) > 0 {
			command = split[0]
			args = split[1:]
		}
	}

	resolved, err := resolveMCPCommand(command, s.searchPaths)
	if err != nil {
		return err
	}

	stdioTransport := transport.NewStdio(resolved, s.env, args...)
	c := client.NewClient(stdioTransport)

	s.mu.Lock()
	s.mcpClient = c
	s.mu.Unlock()

	// Capture subprocess Stderr and pipe it to our logger.
	go s.pipeStderr(stdioTransport.Stderr())

	if err := c.Start(ctx); err != nil {
		return errors.Wrap(err, "start MCP subprocess")
	}

	caps, err := initializeConnectedClient(ctx, s.name, c)
	if err != nil {
		_ = c.Close()
		return err
	}
	caps.Description = preferredServiceDescription(s.description, caps.Description)
	if err := r.RegisterConnectedClient(s.name, c, caps, nil); err != nil {
		_ = c.Close()
		return err
	}

	s.setStatus(StatusRunning)
	s.log.Info().Msg("MCP server started")
	return nil
}

func (s *MCPServer) pipeStderr(r io.Reader) {
	if r == nil {
		return
	}
	scanner := bufio.NewScanner(r)
	// Support lines up to 10MB. Large tool calls/results can easily exceed
	// the default 64KB buffer, causing the scanner to stop and the
	// subprocess to hang on a full pipe.
	const maxLogLineSize = 10 * 1024 * 1024
	scanner.Buffer(make([]byte, 64*1024), maxLogLineSize)

	// Use a small buffer to prevent an I/O storm from blocking the scanner,
	// but process it asynchronously so that slow logging (e.g. to a full disk)
	// doesn't cause a pipe deadlock in the subprocess.
	logCh := make(chan string, 100)
	go func() {
		for line := range logCh {
			if strings.HasPrefix(line, "{") {
				var parsed map[string]any
				if err := json.Unmarshal([]byte(line), &parsed); err == nil {
					lvl := zerolog.DebugLevel
					if lStr, ok := parsed["level"].(string); ok {
						if parsedLvl, err := zerolog.ParseLevel(lStr); err == nil {
							lvl = parsedLvl
						}
					}
					msg, _ := parsed["message"].(string)
					delete(parsed, "level")
					delete(parsed, "message")
					delete(parsed, "time")

					event := s.log.WithLevel(lvl).Str("source", "stderr")
					for k, v := range parsed {
						event.Interface(k, v)
					}
					event.Msg(msg)
					continue
				}
			}
			s.log.Debug().Str("source", "stderr").Msg(line)
		}
	}()

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		select {
		case logCh <- line:
		default:
			// If log buffer is full, we must continue draining the pipe to prevent
			// a deadlock. We skip this log line but avoid hanging the subprocess.
		}
	}
	close(logCh)

	if err := scanner.Err(); err != nil {
		s.log.Error().Err(err).Msg("stderr scanner failed; subprocess may hang if stderr pipe fills")
	}
}

// Stop terminates the MCP server connection.
func (s *MCPServer) Stop() {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	client := s.mcpClient
	s.mcpClient = nil
	s.mu.Unlock()

	if s.stopFn != nil {
		s.stopFn()
	} else if client != nil {
		client.Close()
		s.log.Info().Msg("MCP server stopped")
	}
	s.httpConnected.Store(false)
	s.setStatus(StatusStopped)
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
	initCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    "clara",
		Version: "0.1.0",
	}
	initResult, err := mcpClient.Initialize(initCtx, initReq)
	if err != nil {
		return nil, errors.Wrap(err, "MCP initialize handshake")
	}

	caps := &ServerCapabilities{
		Name:        name,
		Description: preferredServiceDescription("", initResult.Instructions),
	}
	if caps.Description == "" {
		caps.Description = initResult.ServerInfo.Description
	}

	toolsResult, err := mcpClient.ListTools(initCtx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, errors.Wrap(err, "list tools")
	}
	caps.Tools = toolsResult.Tools

	// Resources and prompts are optional capabilities; treat errors as empty.
	if res, err := mcpClient.ListResources(initCtx, mcp.ListResourcesRequest{}); err == nil {
		caps.Resources = res.Resources
	}

	if res, err := mcpClient.ListPrompts(initCtx, mcp.ListPromptsRequest{}); err == nil {
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
