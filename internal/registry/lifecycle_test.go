package registry_test

import (
	"context"
	"testing"

	"github.com/brightpuddle/clara/internal/registry"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"
)

func TestRegistry_Lifecycle(t *testing.T) {
	reg := registry.New(zerolog.Nop())

	started := false
	stopped := false
	srv := registry.NewTestMCPServer("test-srv", func(ctx context.Context, r *registry.Registry) error {
		started = true
		return nil
	}, func() {
		stopped = true
	})

	if err := reg.AddServer(srv); err != nil {
		t.Fatalf("AddServer failed: %v", err)
	}

	// Check initial status
	statuses := reg.ServerStatuses()
	if statuses["test-srv"] != registry.StatusStopped {
		t.Errorf("expected StatusStopped, got %v", statuses["test-srv"])
	}

	// Start server
	if err := reg.StartServer(context.Background(), "test-srv"); err != nil {
		t.Fatalf("StartServer failed: %v", err)
	}

	if !started {
		t.Error("expected server to be started")
	}

	// Status should be Running (assuming Start implementation sets it)
	statuses = reg.ServerStatuses()
	if statuses["test-srv"] != registry.StatusRunning {
		t.Errorf("expected StatusRunning, got %v", statuses["test-srv"])
	}

	// Stop server
	if err := reg.StopServer("test-srv"); err != nil {
		t.Fatalf("StopServer failed: %v", err)
	}

	if !stopped {
		t.Error("expected server to be stopped")
	}

	statuses = reg.ServerStatuses()
	if statuses["test-srv"] != registry.StatusStopped {
		t.Errorf("expected StatusStopped, got %v", statuses["test-srv"])
	}

	// Restart server
	started = false
	stopped = false
	if err := reg.RestartServer(context.Background(), "test-srv"); err != nil {
		t.Fatalf("RestartServer failed: %v", err)
	}

	if !stopped || !started {
		t.Errorf("expected server to be stopped and started, got stopped=%v started=%v", stopped, started)
	}
}

func TestRegistry_StopServerCleansUpTools(t *testing.T) {
	reg := registry.New(zerolog.Nop())

	srv := registry.NewTestMCPServer("test-srv", func(ctx context.Context, r *registry.Registry) error {
		// Mock registration of tools
		r.Register("test-srv.tool1", func(ctx context.Context, args map[string]any) (any, error) {
			return nil, nil
		})
		r.Register("test-srv.tool2", func(ctx context.Context, args map[string]any) (any, error) {
			return nil, nil
		})
		r.Register("other.tool", func(ctx context.Context, args map[string]any) (any, error) {
			return nil, nil
		})
		return nil
	}, func() {})

	if err := reg.AddServer(srv); err != nil {
		t.Fatalf("AddServer failed: %v", err)
	}

	if err := reg.StartServer(context.Background(), "test-srv"); err != nil {
		t.Fatalf("StartServer failed: %v", err)
	}

	if !reg.Has("test-srv.tool1") || !reg.Has("test-srv.tool2") || !reg.Has("other.tool") {
		t.Fatal("expected tools to be registered")
	}

	if err := reg.StopServer("test-srv"); err != nil {
		t.Fatalf("StopServer failed: %v", err)
	}

	if reg.Has("test-srv.tool1") || reg.Has("test-srv.tool2") {
		t.Error("expected test-srv tools to be removed")
	}
	if !reg.Has("other.tool") {
		t.Error("expected other.tool to remain")
	}
}

func TestRegistry_AliasCleanup(t *testing.T) {
	log := zerolog.Nop()
	reg := registry.New(log)

	mcpClient := client.NewClient(nil)
	caps := &registry.ServerCapabilities{
		Name: "macos",
		Tools: []mcp.Tool{
			{
				Name:        "theme_get",
				Description: "Get theme",
			},
		},
	}

	srv := registry.NewTestMCPServer("macos", func(ctx context.Context, r *registry.Registry) error {
		return nil
	}, func() {})
	if err := reg.AddServer(srv); err != nil {
		t.Fatalf("AddServer failed: %v", err)
	}

	err := reg.RegisterConnectedClient("macos", mcpClient, caps, nil)
	if err != nil {
		t.Fatalf("RegisterConnectedClient failed: %v", err)
	}

	if !reg.Has("macos.theme.get") || !reg.Has("theme.get") {
		t.Fatal("expected tools to be registered")
	}

	if err := reg.StopServer("macos"); err != nil {
		t.Fatalf("StopServer failed: %v", err)
	}

	if reg.Has("macos.theme.get") {
		t.Error("expected macos.theme.get to be removed")
	}
	if reg.Has("theme.get") {
		t.Error("expected theme.get to be removed")
	}
}

func TestRegistry_AliasRestartStale(t *testing.T) {
	log := zerolog.Nop()
	reg := registry.New(log)

	srv := registry.NewTestMCPServer("macos", func(ctx context.Context, r *registry.Registry) error {
		return nil
	}, func() {})
	reg.AddServer(srv)

	mcpClient1 := client.NewClient(nil)
	caps1 := &registry.ServerCapabilities{
		Name: "macos",
		Tools: []mcp.Tool{
			{
				Name:        "theme_get",
				Description: "Get theme (v1)",
			},
		},
	}

	reg.RegisterConnectedClient("macos", mcpClient1, caps1, nil)
	reg.StopServer("macos")

	mcpClient2 := client.NewClient(nil)
	caps2 := &registry.ServerCapabilities{
		Name: "macos",
		Tools: []mcp.Tool{
			{
				Name:        "theme_get",
				Description: "Get theme (v2)",
			},
		},
	}
	
	err := reg.RegisterConnectedClient("macos", mcpClient2, caps2, nil)
	if err != nil {
		t.Fatalf("RegisterConnectedClient failed on restart: %v", err)
	}

	info, ok := reg.Tool("macos.theme.get")
	if !ok || info.Description != "Get theme (v2)" {
		t.Errorf("expected v2 description for macos.theme.get")
	}

	info, ok = reg.Tool("theme.get")
	if !ok || info.Description != "Get theme (v2)" {
		t.Errorf("expected v2 description for theme.get, got %q", info.Description)
	}
}
