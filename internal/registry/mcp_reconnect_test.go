package registry

import (
	"context"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
)

// A watchdog restart of an unreachable HTTP MCP server must keep retrying in
// the background and connect once the server returns.
func TestRestartUnresponsive_ReconnectsWhenServerReturns(t *testing.T) {
	old := httpReconnectInterval
	httpReconnectInterval = 50 * time.Millisecond
	t.Cleanup(func() { httpReconnectInterval = old })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() // nothing listens yet: connection refused

	reg := New(zerolog.Nop())
	srv := NewHTTPMCPServer("laptop", "", "http://"+addr+"/mcp", "", false, zerolog.Nop())
	if err := reg.AddServer(srv); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reg.restartUnresponsive(ctx, srv)

	if got := srv.Status(); got != StatusConnecting {
		t.Fatalf("status after failed restart = %v, want connecting", got)
	}

	mcpSrv := server.NewMCPServer("remote", "1", server.WithToolCapabilities(true))
	mcpSrv.AddTool(mcp.NewTool("ping"), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("pong"), nil
	})
	ts := httptest.NewUnstartedServer(server.NewStreamableHTTPServer(mcpSrv))
	l2, err := net.Listen("tcp", addr)
	if err != nil {
		t.Skipf("port %s no longer free: %v", addr, err)
	}
	ts.Listener = l2
	ts.Start()
	defer ts.Close()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if srv.Status() == StatusRunning {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("server never reconnected; status = %v", srv.Status())
}
