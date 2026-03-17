package registry

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
)

func TestDynamicAttachServerRegisterValidation(t *testing.T) {
	reg := New(zerolog.Nop())
	attach := NewDynamicAttachServer(t.TempDir()+"/attach.sock", reg, zerolog.Nop())

	if _, err := attach.Register("tui.notify"); err == nil {
		t.Fatal("expected dotted server name to be rejected")
	}

	if err := reg.AddServer(NewMCPServer("tui", "", "noop", nil, nil, nil, zerolog.Nop())); err != nil {
		t.Fatalf("AddServer failed: %v", err)
	}
	if _, err := attach.Register("tui"); err == nil {
		t.Fatal("expected duplicate server name to be rejected")
	}
}

func TestDynamicAttachServerUnregisterPendingRegistration(t *testing.T) {
	reg := New(zerolog.Nop())
	attach := NewDynamicAttachServer(t.TempDir()+"/attach.sock", reg, zerolog.Nop())

	registration, err := attach.Register("tui")
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if registration.Token == "" {
		t.Fatal("expected registration token")
	}
	if got := attach.Registrations(); len(got) != 1 || got[0] != "tui" {
		t.Fatalf("pending registrations = %v, want [tui]", got)
	}

	if err := attach.Unregister("tui"); err != nil {
		t.Fatalf("Unregister failed: %v", err)
	}
	if got := attach.Registrations(); len(got) != 0 {
		t.Fatalf("pending registrations should be empty, got %v", got)
	}
}

func TestRegistryRegisterConnectedClientLifecycle(t *testing.T) {
	reg := New(zerolog.Nop())
	srv := server.NewMCPServer("peer", "1.0.0")
	srv.AddTool(mcp.NewTool("notify", mcp.WithDescription("Send a TUI notification")), func(
		ctx context.Context,
		request mcp.CallToolRequest,
	) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("ok"), nil
	})

	cli, err := client.NewInProcessClient(srv)
	if err != nil {
		t.Fatalf("NewInProcessClient failed: %v", err)
	}
	if err := cli.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	caps, err := initializeConnectedClient(context.Background(), "tui", cli)
	if err != nil {
		t.Fatalf("initializeConnectedClient failed: %v", err)
	}

	var closed bool
	if err := reg.RegisterConnectedClient("tui", cli, caps, func() error {
		closed = true
		return cli.Close()
	}); err != nil {
		t.Fatalf("RegisterConnectedClient failed: %v", err)
	}

	if !reg.HasServer("tui") {
		t.Fatal("expected dynamic server to be registered")
	}
	if got := reg.DynamicServerNames(); len(got) != 1 || got[0] != "tui" {
		t.Fatalf("DynamicServerNames = %v, want [tui]", got)
	}
	info, ok := reg.Tool("tui.notify")
	if !ok {
		t.Fatal("expected namespaced tool to be registered")
	}
	if info.Description != "Send a TUI notification" {
		t.Fatalf("tool description = %q", info.Description)
	}

	result, err := reg.Call(context.Background(), "tui.notify", nil)
	if err != nil {
		t.Fatalf("dynamic tool call failed: %v", err)
	}
	if text, ok := result.(string); !ok || text != "ok" {
		t.Fatalf("dynamic tool result = %#v, want %q", result, "ok")
	}

	if err := reg.UnregisterDynamicServer("tui"); err != nil {
		t.Fatalf("UnregisterDynamicServer failed: %v", err)
	}
	if !closed {
		t.Fatal("expected unregister to close the dynamic client")
	}
	if reg.HasServer("tui") {
		t.Fatal("expected dynamic server to be removed")
	}
	if _, ok := reg.Tool("tui.notify"); ok {
		t.Fatal("expected dynamic tool to be removed")
	}
}

func TestRegistryRegisterConnectedClientReturnsStructuredContent(t *testing.T) {
	reg := New(zerolog.Nop())
	srv := server.NewMCPServer("peer", "1.0.0")
	srv.AddTool(mcp.NewTool("list"), func(
		ctx context.Context,
		request mcp.CallToolRequest,
	) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultStructuredOnly([]map[string]any{
			{"task_uuid": "t-1"},
		}), nil
	})

	cli, err := client.NewInProcessClient(srv)
	if err != nil {
		t.Fatalf("NewInProcessClient failed: %v", err)
	}
	if err := cli.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	caps, err := initializeConnectedClient(context.Background(), "db", cli)
	if err != nil {
		t.Fatalf("initializeConnectedClient failed: %v", err)
	}

	if err := reg.RegisterConnectedClient("db", cli, caps, cli.Close); err != nil {
		t.Fatalf("RegisterConnectedClient failed: %v", err)
	}

	result, err := reg.Call(context.Background(), "db.list", nil)
	if err != nil {
		t.Fatalf("dynamic structured tool call failed: %v", err)
	}
	rows, ok := result.([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("dynamic structured tool result = %#v", result)
	}
	row, ok := rows[0].(map[string]any)
	if !ok || row["task_uuid"] != "t-1" {
		t.Fatalf("dynamic structured tool result = %#v", result)
	}
}

func TestRegistryRegisterConnectedClientRejectsToolCollision(t *testing.T) {
	reg := New(zerolog.Nop())
	reg.Register(
		"tui.notify",
		func(context.Context, map[string]any) (any, error) { return nil, nil },
	)

	srv := server.NewMCPServer("peer", "1.0.0")
	srv.AddTool(mcp.NewTool("notify"), func(
		ctx context.Context,
		request mcp.CallToolRequest,
	) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("ok"), nil
	})

	cli, err := client.NewInProcessClient(srv)
	if err != nil {
		t.Fatalf("NewInProcessClient failed: %v", err)
	}
	if err := cli.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	caps, err := initializeConnectedClient(context.Background(), "tui", cli)
	if err != nil {
		t.Fatalf("initializeConnectedClient failed: %v", err)
	}

	if err := reg.RegisterConnectedClient("tui", cli, caps, cli.Close); err == nil {
		t.Fatal("expected tool collision to be rejected")
	}
	if reg.HasServer("tui") {
		t.Fatal("expected failed registration to leave no server alias behind")
	}
}
