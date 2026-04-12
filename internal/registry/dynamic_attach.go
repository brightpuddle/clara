package registry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"os"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/client"
	"github.com/rs/zerolog"
)

var dynamicServerNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

type DynamicRegistration struct {
	Name       string `json:"name"`
	Token      string `json:"token"`
	SocketPath string `json:"socket_path"`
}

type DynamicAttachServer struct {
	socketPath string
	reg        *Registry
	log        zerolog.Logger

	mu            sync.Mutex
	pendingByName map[string]string
	pendingByTok  map[string]string
}

type attachRequest struct {
	Token string `json:"token"`
}

type attachResponse struct {
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

func NewDynamicAttachServer(
	socketPath string,
	reg *Registry,
	log zerolog.Logger,
) *DynamicAttachServer {
	return &DynamicAttachServer{
		socketPath:    socketPath,
		reg:           reg,
		log:           log.With().Str("socket", socketPath).Logger(),
		pendingByName: make(map[string]string),
		pendingByTok:  make(map[string]string),
	}
}

func (s *DynamicAttachServer) ListenAndServe(ctx context.Context) error {
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		return errors.Wrap(err, "remove stale dynamic MCP socket")
	}

	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return errors.Wrap(err, "listen on dynamic MCP socket")
	}
	defer func() {
		_ = ln.Close()
		_ = os.Remove(s.socketPath)
	}()

	s.log.Info().Msg("dynamic MCP attach server listening")

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			s.log.Warn().Err(err).Msg("accept dynamic MCP peer")
			continue
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *DynamicAttachServer) Register(name string) (*DynamicRegistration, error) {
	if !dynamicServerNamePattern.MatchString(name) {
		return nil, errors.Newf("invalid MCP server name %q", name)
	}
	if s.reg.HasServer(name) {
		return nil, errors.Newf("MCP server %q already registered", name)
	}

	token, err := randomToken()
	if err != nil {
		return nil, errors.Wrap(err, "generate registration token")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if oldToken, ok := s.pendingByName[name]; ok {
		delete(s.pendingByTok, oldToken)
	}
	s.pendingByName[name] = token
	s.pendingByTok[token] = name

	return &DynamicRegistration{
		Name:       name,
		Token:      token,
		SocketPath: s.socketPath,
	}, nil
}

func (s *DynamicAttachServer) Unregister(name string) error {
	s.mu.Lock()
	if token, ok := s.pendingByName[name]; ok {
		delete(s.pendingByName, name)
		delete(s.pendingByTok, token)
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	return s.reg.UnregisterDynamicServer(name)
}

func (s *DynamicAttachServer) Registrations() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	names := make([]string, 0, len(s.pendingByName))
	for name := range s.pendingByName {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (s *DynamicAttachServer) handleConn(ctx context.Context, conn net.Conn) {
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		s.log.Warn().Err(err).Msg("set attach handshake deadline")
	}

	var req attachRequest
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		if !errors.Is(err, io.EOF) {
			s.log.Warn().Err(err).Msg("decode dynamic attach request")
		}
		_ = conn.Close()
		return
	}

	name, ok := s.consumeToken(req.Token)
	if !ok {
		_ = json.NewEncoder(conn).
			Encode(attachResponse{Error: "invalid or expired registration token"})
		_ = conn.Close()
		return
	}

	if err := json.NewEncoder(conn).Encode(attachResponse{Message: "ready"}); err != nil {
		s.log.Warn().Err(err).Str("server", name).Msg("encode dynamic attach response")
		_ = conn.Close()
		return
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		s.log.Warn().Err(err).Str("server", name).Msg("clear attach deadline")
	}

	transport := NewConnTransport(conn)
	mcpClient := client.NewClient(transport)
	if err := mcpClient.Start(ctx); err != nil {
		s.log.Warn().Err(err).Str("server", name).Msg("start dynamic MCP transport")
		_ = conn.Close()
		return
	}

	caps, err := initializeConnectedClient(ctx, name, mcpClient)
	if err != nil {
		s.log.Warn().Err(err).Str("server", name).Msg("initialize dynamic MCP peer")
		_ = transport.Close()
		return
	}
	if err := s.reg.RegisterConnectedClient(name, mcpClient, caps, transport.Close); err != nil {
		s.log.Warn().Err(err).Str("server", name).Msg("register dynamic MCP peer")
		_ = transport.Close()
		return
	}

	s.log.Info().Str("server", name).Int("tools", len(caps.Tools)).Msg("dynamic MCP peer attached")

	if err := <-transport.Done(); err != nil {
		s.log.Warn().Err(err).Str("server", name).Msg("dynamic MCP peer disconnected")
	}
	s.reg.CleanupDynamicServer(name)
}

func (s *DynamicAttachServer) consumeToken(token string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	name, ok := s.pendingByTok[token]
	if !ok {
		return "", false
	}
	delete(s.pendingByTok, token)
	delete(s.pendingByName, name)
	return name, true
}

func randomToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
