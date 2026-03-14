package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/brightpuddle/clara/internal/config"
)

func TestLoad_BasicParsing(t *testing.T) {
	yaml := `
log_level: debug
data_dir: /tmp/clara-test
mcp_servers:
  - name: filesystem
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem"]
    env:
      ROOT: /tmp
llm:
  provider: openai
  model: gpt-4o
`
	f := writeTempFile(t, yaml)
	cfg, err := config.Load(f)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel: got %q want %q", cfg.LogLevel, "debug")
	}
	if cfg.DataDir != "/tmp/clara-test" {
		t.Errorf("DataDir: got %q want %q", cfg.DataDir, "/tmp/clara-test")
	}
	if len(cfg.MCPServers) != 1 {
		t.Fatalf("expected 1 MCP server, got %d", len(cfg.MCPServers))
	}
	srv := cfg.MCPServers[0]
	if srv.Name != "filesystem" {
		t.Errorf("server name: got %q want %q", srv.Name, "filesystem")
	}
	if len(srv.Args) != 2 {
		t.Errorf("args len: got %d want 2", len(srv.Args))
	}
	if cfg.LLM.Model != "gpt-4o" {
		t.Errorf("LLM model: got %q want %q", cfg.LLM.Model, "gpt-4o")
	}
}

func TestLoad_EnvExpansion(t *testing.T) {
	t.Setenv("CLARA_TEST_API_KEY", "secret-key-123")
	yaml := `
mcp_servers:
  - name: openai
    command: openai-mcp
    env:
      OPENAI_API_KEY: ${CLARA_TEST_API_KEY}
`
	f := writeTempFile(t, yaml)
	cfg, err := config.Load(f)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	env := cfg.MCPServers[0].ResolvedEnv()
	if env["OPENAI_API_KEY"] != "secret-key-123" {
		t.Errorf("env expansion: got %q want %q", env["OPENAI_API_KEY"], "secret-key-123")
	}
}

func TestLoad_Defaults(t *testing.T) {
	yaml := `log_level: warn`
	f := writeTempFile(t, yaml)
	cfg, err := config.Load(f)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.DataDir == "" {
		t.Error("DataDir should have a default value")
	}
}

func TestLoad_DefaultLogLevel(t *testing.T) {
	yaml := `data_dir: /tmp`
	f := writeTempFile(t, yaml)
	cfg, err := config.Load(f)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("default log level: got %q want %q", cfg.LogLevel, "info")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	f := writeTempFile(t, "not: valid: yaml: :")
	_, err := config.Load(f)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := config.Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestConfigDerivedPaths(t *testing.T) {
	yaml := `data_dir: /tmp/clara-paths`
	f := writeTempFile(t, yaml)
	cfg, err := config.Load(f)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DBPath() != "/tmp/clara-paths/clara.db" {
		t.Errorf("DBPath: got %q", cfg.DBPath())
	}
	if cfg.ControlSocketPath() != "/tmp/clara-paths/clara.sock" {
		t.Errorf("ControlSocketPath: got %q", cfg.ControlSocketPath())
	}
	if cfg.DynamicMCPSocketPath() != "/tmp/clara-paths/clara-mcp.sock" {
		t.Errorf("DynamicMCPSocketPath: got %q", cfg.DynamicMCPSocketPath())
	}
	if cfg.TasksDir() != "/tmp/clara-paths/tasks" {
		t.Errorf("TasksDir: got %q", cfg.TasksDir())
	}
	if cfg.LogPath() != "/tmp/clara-paths/clara.log" {
		t.Errorf("LogPath: got %q", cfg.LogPath())
	}
}

func TestLogLevelNormalized(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"INFO", "info"},
		{"  Warn  ", "warn"},
		{"DEBUG", "debug"},
	}
	for _, tc := range cases {
		yaml := "log_level: " + tc.input
		f := writeTempFile(t, yaml)
		loaded, err := config.Load(f)
		if err != nil {
			t.Fatalf("Load(%q): %v", tc.input, err)
		}
		if got := loaded.LogLevelNormalized(); got != tc.want {
			t.Errorf("LogLevelNormalized(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}
