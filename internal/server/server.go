package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/brightpuddle/clara/internal/config"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
)

// HTTPDispatcher is the interface implemented by pluginLoader to route webhooks.
type HTTPDispatcher interface {
	DispatchHTTP(pluginName, method, path string, headers map[string]string, body []byte) (int, []byte, error)
}

type Server struct {
	cfg        *config.Config
	reg        *registry.Registry
	dispatcher HTTPDispatcher
	log        zerolog.Logger
	httpServer *http.Server
}

func New(cfg *config.Config, reg *registry.Registry, dispatcher HTTPDispatcher, log zerolog.Logger) *Server {
	return &Server{
		cfg:        cfg,
		reg:        reg,
		dispatcher: dispatcher,
		log:        log.With().Str("component", "http_server").Logger(),
	}
}

func (s *Server) Start() error {
	if s.cfg.Server.ListenAddr == "" {
		return nil
	}

	mux := http.NewServeMux()

	// Authentication middleware
	auth := func(next http.Handler) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if s.cfg.Server.SharedSecret != "" {
				authHeader := r.Header.Get("Authorization")
				if authHeader != "Bearer "+s.cfg.Server.SharedSecret {
					http.Error(w, "Unauthorized", http.StatusUnauthorized)
					return
				}
			}
			next.ServeHTTP(w, r)
		}
	}

	authFunc := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if s.cfg.Server.SharedSecret != "" {
				authHeader := r.Header.Get("Authorization")
				if authHeader != "Bearer "+s.cfg.Server.SharedSecret {
					http.Error(w, "Unauthorized", http.StatusUnauthorized)
					return
				}
			}
			next(w, r)
		}
	}

	// MCP SSE Server
	mcpSrv := mcpserver.NewMCPServer("clara-remote", "1.0.0")

	// Register all tools from registry to the MCP server
	// AddTool is standard in mcp-go
	for _, info := range s.reg.Tools() {
		info := info // capture loop variable
		mcpSrv.AddTool(info.Spec, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args map[string]any
			if request.Params.Arguments != nil {
				if m, ok := request.Params.Arguments.(map[string]any); ok {
					args = m
				}
			}
			if args == nil {
				args = make(map[string]any)
			}
			result, err := s.reg.Call(ctx, info.Name, args)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			resBytes, err := json.Marshal(result)
			if err != nil {
				return nil, err
			}
			return mcp.NewToolResultText(string(resBytes)), nil
		})
	}

	sseServer := mcpserver.NewSSEServer(mcpSrv)
	mux.Handle("/mcp/sse", auth(sseServer.SSEHandler()))
	mux.Handle("/mcp/messages", auth(sseServer.MessageHandler()))

	// Events bridging
	mux.HandleFunc("/events", authFunc(s.handleEvents))

	// Plugin HTTP proxy
	mux.HandleFunc("/api/", s.handlePluginAPI)
	mux.HandleFunc("/auth/", s.handlePluginAPI)

	s.httpServer = &http.Server{
		Addr:    s.cfg.Server.ListenAddr,
		Handler: mux,
	}

	s.log.Info().Str("addr", s.cfg.Server.ListenAddr).Msg("starting Clara HTTP server")
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Error().Err(err).Msg("HTTP server failed")
		}
	}()

	return nil
}

func (s *Server) Stop(ctx context.Context) error {
	if s.httpServer != nil {
		s.log.Info().Msg("stopping Clara HTTP server")
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	evChan := make(chan struct {
		ServerName string
		Method     string
		Params     any
	}, 64)

	s.reg.Subscribe(func(serverName, method string, params any) {
		select {
		case evChan <- struct{ServerName, Method string; Params any}{serverName, method, params}:
		default:
		}
	})

	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-evChan:
			data, _ := json.Marshal(ev.Params)
			eventName := fmt.Sprintf("%s.%s", ev.ServerName, ev.Method)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventName, data)
			flusher.Flush()
		}
	}
}

func (s *Server) handlePluginAPI(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if len(parts) < 2 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	pluginName := parts[1] // e.g., webex from /api/webex

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	headers := make(map[string]string)
	for k, v := range r.Header {
		headers[k] = v[0]
	}

	status, respBody, err := s.dispatcher.DispatchHTTP(pluginName, r.Method, r.URL.Path, headers, body)
	if err != nil {
		s.log.Error().Err(err).Str("plugin", pluginName).Msg("plugin HTTP dispatch failed")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(status)
	w.Write(respBody)
}
