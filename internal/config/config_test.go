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
    command: npx -y @modelcontextprotocol/server-filesystem
    env:
      ROOT: /tmp
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
	if srv.Command != "npx -y @modelcontextprotocol/server-filesystem" {
		t.Errorf("command: got %q want %q", srv.Command, "npx -y @modelcontextprotocol/server-filesystem")
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
	if got := cfg.MCPCommandSearchPathList(); len(got) == 0 {
		t.Fatal("MCPCommandSearchPathList should include defaults")
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
	yaml := `
data_dir: /tmp/clara-paths
tasks_dir: /tmp/clara-paths/tasks
`
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

func TestMCPCommandSearchPathList_PrependsConfiguredPaths(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	yaml := `
mcp_command_search_paths:
  - /custom/bin
  - /usr/local/bin
`
	f := writeTempFile(t, yaml)
	cfg, err := config.Load(f)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	got := cfg.MCPCommandSearchPathList()
	wantPrefix := []string{"/custom/bin", "/usr/local/bin", "/opt/homebrew/bin", "/usr/bin", "/bin"}
	if len(got) < len(wantPrefix) {
		t.Fatalf("search path list too short: %v", got)
	}
	for i, want := range wantPrefix {
		if got[i] != want {
			t.Fatalf("path %d = %q, want %q (full=%v)", i, got[i], want, got)
		}
	}
}

func TestLoad_HTTPServerConfig(t *testing.T) {
	yaml := `
mcp_servers:
  - name: chrome
    url: "http://127.0.0.1:12306/mcp"
    description: "Chrome browser automation"
  - name: filesystem
    command: npx -y @modelcontextprotocol/server-filesystem
`
	f := writeTempFile(t, yaml)
	cfg, err := config.Load(f)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(cfg.MCPServers) != 2 {
		t.Fatalf("expected 2 MCP servers, got %d", len(cfg.MCPServers))
	}

	chrome := cfg.MCPServers[0]
	if chrome.Name != "chrome" {
		t.Errorf("name: got %q want %q", chrome.Name, "chrome")
	}
	if chrome.URL != "http://127.0.0.1:12306/mcp" {
		t.Errorf("url: got %q want %q", chrome.URL, "http://127.0.0.1:12306/mcp")
	}
	if !chrome.IsHTTPServer() {
		t.Error("IsHTTPServer should be true when URL is set")
	}

	fs := cfg.MCPServers[1]
	if fs.IsHTTPServer() {
		t.Error("IsHTTPServer should be false when only command is set")
	}
	if fs.Command != "npx -y @modelcontextprotocol/server-filesystem" {
		t.Errorf("command: got %q want %q", fs.Command, "npx -y @modelcontextprotocol/server-filesystem")
	}
}

func TestCommandArgs(t *testing.T) {
	cases := []struct {
		name    string
		command string
		want    []string
	}{
		{
			"simple command",
			"ls -l",
			[]string{"ls", "-l"},
		},
		{
			"complex command with quotes",
			`npx -y @modelcontextprotocol/server-filesystem "/path with spaces"`,
			[]string{"npx", "-y", "@modelcontextprotocol/server-filesystem", "/path with spaces"},
		},
		{
			"empty command",
			"",
			nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := config.MCPServerConfig{Command: tc.command}
			got, err := srv.CommandArgs()
			if err != nil {
				t.Fatalf("CommandArgs failed: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d args, want %d: %v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("arg %d: got %q want %q", i, got[i], tc.want[i])
				}
			}
		})
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
