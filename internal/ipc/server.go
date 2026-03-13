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

// Handler processes a CLI Request and returns a Response.
type Handler interface {
	Handle(ctx context.Context, req *Request) *Response
}

// HandlerFunc is a function that implements Handler.
type HandlerFunc func(ctx context.Context, req *Request) *Response

func (f HandlerFunc) Handle(ctx context.Context, req *Request) *Response {
	return f(ctx, req)
}

// Server listens on a Unix Domain Socket and dispatches requests to a Handler.
type Server struct {
	socketPath string
	handler    Handler
	log        zerolog.Logger
}

// NewServer creates a new control socket server.
func NewServer(socketPath string, handler Handler, log zerolog.Logger) *Server {
	return &Server{socketPath: socketPath, handler: handler, log: log}
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

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		if errors.Is(err, io.EOF) {
			return
		}
		s.log.Warn().Err(err).Msg("decode request")
		return
	}

	resp := s.handler.Handle(ctx, &req)
	if err := json.NewEncoder(conn).Encode(resp); err != nil {
		s.log.Warn().Err(err).Msg("encode response")
	}
}
