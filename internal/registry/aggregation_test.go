package registry

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
)

func TestNamespaceAggregation(t *testing.T) {
	reg := New(zerolog.Nop())

	// Simulate multiple servers registering tools in the same namespace
	reg.Register("mail.search", func(ctx context.Context, args map[string]any) (any, error) {
		return "search result", nil
	})
	reg.Register("mail.list", func(ctx context.Context, args map[string]any) (any, error) {
		return "list result", nil
	})

	// Check namespaces
	namespaces := reg.Namespaces()
	foundMail := false
	for _, ns := range namespaces {
		if ns == "mail" {
			foundMail = true
			break
		}
	}
	if !foundMail {
		t.Errorf("expected mail namespace, got %v", namespaces)
	}

	// Verify we can call both
	res1, err := reg.Call(context.Background(), "mail.search", nil)
	if err != nil { t.Fatalf("mail.search failed: %v", err) }
	if res1 != "search result" { t.Errorf("got %v", res1) }

	res2, err := reg.Call(context.Background(), "mail.list", nil)
	if err != nil { t.Fatalf("mail.list failed: %v", err) }
	if res2 != "list result" { t.Errorf("got %v", res2) }
}

func TestDualNamespacingAndConflict(t *testing.T) {
	reg := New(zerolog.Nop())

	// 1. Manually register a tool to simulate conflict on the "clean" name
	reg.Register("reminders.list", func(ctx context.Context, args map[string]any) (any, error) {
		return "v1", nil
	})

	// 2. Register an MCP server "macos" which has a tool that maps to the same name
	// via hardcoded defaults (reminders_list -> reminders.list)
	srv := server.NewMCPServer("macos", "1.0.0")
	cli, err := client.NewInProcessClient(srv)
	if err != nil {
		t.Fatalf("NewInProcessClient failed: %v", err)
	}
	caps := &ServerCapabilities{
		Name: "macos",
		Tools: []mcp.Tool{
			mcp.NewTool("reminders_list"),
		},
	}

	// This should NOT fail now with dual registration, but it will fail on current implementation
	if err := reg.RegisterConnectedClient("macos", cli, caps, nil); err != nil {
		t.Fatalf("RegisterConnectedClient failed: %v", err)
	}

	// 3. Verify dual registration
	// Both clean name and server-prefixed name should be registered (if not conflicting)
	// In this case, macos.reminders.list should be registered.
	if !reg.Has("macos.reminders.list") {
		t.Error("expected macos.reminders.list to be registered")
	}

	// 4. Verify conflict resolution: reminders.list should still be v1
	res, err := reg.Call(context.Background(), "reminders.list", nil)
	if err != nil {
		t.Fatalf("Call reminders.list failed: %v", err)
	}
	if res != "v1" {
		t.Errorf("reminders.list should not have been overwritten, got %v", res)
	}

	// 5. Verify a new tool without conflict registers both
	srv2 := server.NewMCPServer("weather-service", "1.0.0")
	cli2, err := client.NewInProcessClient(srv2)
	if err != nil {
		t.Fatalf("NewInProcessClient failed: %v", err)
	}
	caps2 := &ServerCapabilities{
		Name: "weather-service",
		Tools: []mcp.Tool{
			mcp.NewTool("get_temp"),
		},
	}
	if err := reg.RegisterConnectedClient("weather-service", cli2, caps2, nil); err != nil {
		t.Fatalf("RegisterConnectedClient weather-service failed: %v", err)
	}

	// cleanName = weather-service.get_temp
	// nsName = weather-service.get_temp (because it starts with weather-service.)
	
	srv3 := server.NewMCPServer("clara-bridge", "1.0.0")
	cli3, _ := client.NewInProcessClient(srv3)
	caps3 := &ServerCapabilities{
		Name: "clara-bridge",
		Tools: []mcp.Tool{
			mcp.NewTool("reminders_create"), // becomes reminders.create
		},
	}
	if err := reg.RegisterConnectedClient("clara-bridge", cli3, caps3, nil); err != nil {
		t.Fatalf("RegisterConnectedClient clara-bridge failed: %v", err)
	}

	// cleanName = reminders.create
	// nsName = clara-bridge.reminders.create
	if !reg.Has("reminders.create") {
		t.Error("expected reminders.create to be registered")
	}
	if !reg.Has("clara-bridge.reminders.create") {
		t.Error("expected clara-bridge.reminders.create to be registered")
	}
}
