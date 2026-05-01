package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"
)

func TestHandleConnIgnoresEOFProbe(t *testing.T) {
	var logBuf bytes.Buffer
	logger := zerolog.New(&logBuf)
	server, err := NewServer(
		"",
		HandlerFunc(func(ctx context.Context, req *Request, w ResponseWriter) {
			t.Fatal("handler should not be called for an empty probe connection")
		}),
		logger,
	)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	serverConn, clientConn := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.handleConn(context.Background(), serverConn)
	}()

	if err := clientConn.Close(); err != nil {
		t.Fatalf("close probe connection: %v", err)
	}
	<-done

	if got := logBuf.String(); got != "" {
		t.Fatalf("expected no log output for EOF probe, got %q", got)
	}
}

func TestHandleConnLogsMalformedRequest(t *testing.T) {
	var logBuf bytes.Buffer
	logger := zerolog.New(&logBuf)
	var called atomic.Bool
	server, err := NewServer(
		"",
		HandlerFunc(func(ctx context.Context, req *Request, w ResponseWriter) {
			called.Store(true)
			_ = w.Write(&Response{Message: "unexpected"})
		}),
		logger,
	)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	serverConn, clientConn := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.handleConn(context.Background(), serverConn)
	}()

	if _, err := clientConn.Write([]byte("{")); err != nil {
		t.Fatalf("write malformed request: %v", err)
	}
	if err := clientConn.Close(); err != nil {
		t.Fatalf("close malformed request connection: %v", err)
	}
	<-done

	if called.Load() {
		t.Fatal("handler should not be called for malformed JSON")
	}
	if got := logBuf.String(); !strings.Contains(got, "\"decode request\"") {
		t.Fatalf("expected malformed request to be logged, got %q", got)
	}
}

func TestHandleConnServesValidRequest(t *testing.T) {
	var logBuf bytes.Buffer
	logger := zerolog.New(&logBuf)
	server, err := NewServer(
		"",
		HandlerFunc(func(ctx context.Context, req *Request, w ResponseWriter) {
			if req.Method != MethodStatus {
				t.Fatalf("unexpected method: got %q want %q", req.Method, MethodStatus)
			}
			_ = w.Write(&Response{Message: "running", Data: map[string]any{"tools": 3}})
		}),
		logger,
	)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	serverConn, clientConn := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.handleConn(context.Background(), serverConn)
	}()

	if err := json.NewEncoder(clientConn).Encode(Request{Method: MethodStatus}); err != nil {
		t.Fatalf("encode request: %v", err)
	}

	var resp Response
	if err := json.NewDecoder(clientConn).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if err := clientConn.Close(); err != nil {
		t.Fatalf("close client connection: %v", err)
	}
	<-done

	if resp.Message != "running" {
		t.Fatalf("response message: got %q want %q", resp.Message, "running")
	}

	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("response data type: got %T want map[string]any", resp.Data)
	}
	if got := data["tools"]; got != float64(3) {
		t.Fatalf("response tools: got %v want %v", got, float64(3))
	}
	if got := logBuf.String(); got != "" {
		t.Fatalf("expected no log output for valid request, got %q", got)
	}
}
