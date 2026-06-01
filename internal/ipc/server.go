// Package ipc provides the control socket server run by the daemon and the
// protocol types shared with the CLI client.
package ipc

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"

	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"
)

// ResponseWriter allows a handler to send one or more responses to the client.
type ResponseWriter interface {
	Write(resp *Response) error
}

// RawWriter allows a handler to write raw newline-delimited JSON to the client.
// Used by streaming log commands (event.logs, evaluator.logs, actuator.logs).
type RawWriter interface {
	WriteRaw(line []byte) error
}

// StreamHandler processes a streaming CLI request, writing raw JSON lines via
// RawWriter until the context is cancelled or an error occurs.
type StreamHandler interface {
	HandleStream(ctx context.Context, req *StreamRequest, w RawWriter)
}

// Handler processes a CLI Request and returns a Response via ResponseWriter.
type Handler interface {
	Handle(ctx context.Context, req *Request, w ResponseWriter)
	// HandleStream is called for streaming requests.
	HandleStream(ctx context.Context, req *StreamRequest, w RawWriter)
}

// HandlerFunc is a function that implements Handler (non-streaming only).
// For streaming support, use a struct that implements the full Handler interface.
type HandlerFunc func(ctx context.Context, req *Request, w ResponseWriter)

func (f HandlerFunc) Handle(ctx context.Context, req *Request, w ResponseWriter) {
	f(ctx, req, w)
}

func (f HandlerFunc) HandleStream(_ context.Context, _ *StreamRequest, _ RawWriter) {
	// No-op: HandlerFunc does not support streaming.
}

// Server listens on a Unix Domain Socket and dispatches requests to a Handler.
type Server struct {
	socketPath string
	handler    Handler
	log        zerolog.Logger
}

// NewServer creates a new control socket server.
func NewServer(socketPath string, handler Handler, log zerolog.Logger) (*Server, error) {
	if len(socketPath) > 104 {
		return nil, errors.Newf(
			"unix socket path too long: %s (length: %d, max: 104)",
			socketPath,
			len(socketPath),
		)
	}
	return &Server{socketPath: socketPath, handler: handler, log: log}, nil
}

// ListenAndServe starts the server and blocks until ctx is cancelled.
// It removes any stale socket file before binding.
func (s *Server) ListenAndServe(ctx context.Context) error {
	// Remove stale socket if present.
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		return errors.Wrap(err, "remove stale socket")
	}

	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return errors.Wrap(err, "listen on control socket")
	}
	defer func() {
		ln.Close()
		os.Remove(s.socketPath) //nolint:errcheck
	}()

	s.log.Info().Str("socket", s.socketPath).Msg("control server listening")

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil // clean shutdown
			}
			s.log.Warn().Err(err).Msg("accept error")
			continue
		}
		go s.handleConn(ctx, conn)
	}
}

// streamMethods is the set of IPC methods that use the streaming wire protocol.
var streamMethods = map[string]bool{
	MethodEventLogs:     true,
	MethodEvaluatorLogs: true,
	MethodActuatorLogs:  true,
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	// Peek at method to decide which protocol to use.
	dec := json.NewDecoder(conn)

	// Try streaming request first (it has a superset of fields).
	var peek struct {
		Method string `json:"method"`
	}
	// We need to decode the full message; use a generic map.
	var raw map[string]json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		if errors.Is(err, io.EOF) {
			return
		}
		s.log.Warn().Err(err).Msg("decode request")
		return
	}
	if m, ok := raw["method"]; ok {
		_ = json.Unmarshal(m, &peek.Method)
	}

	if streamMethods[peek.Method] {
		// Re-decode as StreamRequest.
		fullBytes, _ := json.Marshal(raw)
		var sreq StreamRequest
		_ = json.Unmarshal(fullBytes, &sreq)
		// Default tail.
		if sreq.Tail == 0 {
			sreq.Tail = 50
		}
		connCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		rw := &rawWriter{conn: conn, cancel: cancel}
		s.handler.HandleStream(connCtx, &sreq, rw)
		return
	}

	// Regular request/response.
	fullBytes, _ := json.Marshal(raw)
	var req Request
	_ = json.Unmarshal(fullBytes, &req)
	w := &responseWriter{encoder: json.NewEncoder(conn)}
	s.handler.Handle(ctx, &req, w)
}

type responseWriter struct {
	encoder *json.Encoder
}

func (w *responseWriter) Write(resp *Response) error {
	return w.encoder.Encode(resp)
}

type rawWriter struct {
	conn   net.Conn
	cancel context.CancelFunc
}

func (w *rawWriter) WriteRaw(line []byte) error {
	// Append newline delimiter if not present.
	if len(line) == 0 || line[len(line)-1] != '\n' {
		line = append(line, '\n')
	}
	_, err := w.conn.Write(line)
	if err != nil {
		w.cancel()
	}
	return err
}
