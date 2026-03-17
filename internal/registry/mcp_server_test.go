package registry

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"
)

func TestPreferredServiceDescription(t *testing.T) {
	if got := preferredServiceDescription("Configured description", "Discovered description"); got != "Configured description" {
		t.Fatalf("configured description should win, got %q", got)
	}

	if got := preferredServiceDescription("", "Discovered description"); got != "Discovered description" {
		t.Fatalf("discovered description should be used as fallback, got %q", got)
	}
}

func TestResolveMCPCommand_UsesConfiguredSearchPaths(t *testing.T) {
	dir := t.TempDir()
	commandPath := filepath.Join(dir, "example-mcp")
	if err := os.WriteFile(commandPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write command: %v", err)
	}

	got, err := resolveMCPCommand("example-mcp", []string{dir})
	if err != nil {
		t.Fatalf("resolveMCPCommand failed: %v", err)
	}
	if got != commandPath {
		t.Fatalf("resolved path = %q, want %q", got, commandPath)
	}
}

func TestResolveMCPCommand_PreservesExplicitPaths(t *testing.T) {
	commandPath := filepath.Join(t.TempDir(), "example-mcp")
	got, err := resolveMCPCommand(commandPath, nil)
	if err != nil {
		t.Fatalf("resolveMCPCommand failed: %v", err)
	}
	if got != commandPath {
		t.Fatalf("resolved path = %q, want %q", got, commandPath)
	}
}

func TestBuildServerEnvPrependsSearchPathsToPATH(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")

	env := buildServerEnv(
		map[string]string{"FOO": "bar"},
		[]string{"/custom/bin", "/usr/local/bin"},
	)
	var pathValue string
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if key == "PATH" {
			pathValue = value
			break
		}
	}
	if pathValue == "" {
		t.Fatal("expected PATH in environment")
	}
	if !strings.HasPrefix(pathValue, "/custom/bin:/usr/local/bin:/usr/bin:/bin") {
		t.Fatalf("unexpected PATH value: %q", pathValue)
	}
}

func TestRegistryStartServersContinuesAfterServerFailure(t *testing.T) {
	reg := New(zerolog.Nop())

	failed := &MCPServer{
		name: "broken",
		startFn: func(context.Context, *Registry) error {
			return errors.New("boom")
		},
	}
	started := false
	ok := &MCPServer{
		name: "working",
		startFn: func(context.Context, *Registry) error {
			started = true
			return nil
		},
	}

	if err := reg.AddServer(failed); err != nil {
		t.Fatalf("AddServer failed: %v", err)
	}
	if err := reg.AddServer(ok); err != nil {
		t.Fatalf("AddServer failed: %v", err)
	}

	if err := reg.StartServers(context.Background()); err != nil {
		t.Fatalf("StartServers failed: %v", err)
	}
	if !started {
		t.Fatal("expected later MCP servers to still start after an earlier failure")
	}
}
