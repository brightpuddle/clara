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
integrations:
  fs:
    root: /tmp
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
	if len(cfg.Integrations) != 1 {
		t.Fatalf("expected 1 integration, got %d", len(cfg.Integrations))
	}
	fs, ok := cfg.Integrations["fs"]
	if !ok {
		t.Fatal("expected 'fs' integration")
	}
	if fs["root"] != "/tmp" {
		t.Errorf("fs root: got %v want %q", fs["root"], "/tmp")
	}
}

func TestLoad_EnvExpansion(t *testing.T) {
	t.Setenv("CLARA_TEST_API_KEY", "secret-key-123")
	yaml := `
integrations:
  shell:
    api_key: ${CLARA_TEST_API_KEY}
`
	f := writeTempFile(t, yaml)
	cfg, err := config.Load(f)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	shell := cfg.Integrations["shell"]
	if shell["api_key"] != "secret-key-123" {
		t.Errorf("env expansion: got %q want %q", shell["api_key"], "secret-key-123")
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
	yaml := `
data_dir: /tmp/clara-paths
task_dirs:
  - /tmp/clara-paths/tasks
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
	if len(cfg.TaskDirs()) != 1 || cfg.TaskDirs()[0] != "/tmp/clara-paths/tasks" {
		t.Errorf("TaskDirs: got %v", cfg.TaskDirs())
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

func TestLoad_PluginsWhitelist(t *testing.T) {
	yaml := `
plugins:
  - name: chrome
  - name: llm
  - name: macos
    path: /usr/local/libexec/ClaraBridge.app/Contents/MacOS/ClaraBridge
`
	f := writeTempFile(t, yaml)
	cfg, err := config.Load(f)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(cfg.Plugins) != 3 {
		t.Fatalf("Plugins: got %d want 3", len(cfg.Plugins))
	}
	if cfg.Plugins[0].Name != "chrome" {
		t.Errorf("Plugins[0].Name: got %q want %q", cfg.Plugins[0].Name, "chrome")
	}
	if cfg.Plugins[1].Name != "llm" {
		t.Errorf("Plugins[1].Name: got %q want %q", cfg.Plugins[1].Name, "llm")
	}
	if cfg.Plugins[2].Name != "macos" {
		t.Errorf("Plugins[2].Name: got %q want %q", cfg.Plugins[2].Name, "macos")
	}
	wantPath := "/usr/local/libexec/ClaraBridge.app/Contents/MacOS/ClaraBridge"
	if cfg.Plugins[2].Path != wantPath {
		t.Errorf("Plugins[2].Path: got %q want %q", cfg.Plugins[2].Path, wantPath)
	}
}

func TestLoad_PluginSearchPathsDefault(t *testing.T) {
	yaml := `log_level: info`
	f := writeTempFile(t, yaml)
	cfg, err := config.Load(f)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(cfg.PluginSearchPaths) == 0 {
		t.Fatal("PluginSearchPaths should have defaults")
	}
	// First default path should be the integrations directory.
	if !filepath.IsAbs(cfg.PluginSearchPaths[0]) {
		t.Errorf("PluginSearchPaths[0] should be absolute, got %q", cfg.PluginSearchPaths[0])
	}
}

func TestLoad_PluginSearchPathsOverride(t *testing.T) {
	yaml := `
plugin_search_paths:
  - /opt/clara/plugins
  - /usr/local/libexec
`
	f := writeTempFile(t, yaml)
	cfg, err := config.Load(f)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(cfg.PluginSearchPaths) != 2 {
		t.Fatalf("PluginSearchPaths: got %d want 2", len(cfg.PluginSearchPaths))
	}
	if cfg.PluginSearchPaths[0] != "/opt/clara/plugins" {
		t.Errorf(
			"PluginSearchPaths[0]: got %q want %q",
			cfg.PluginSearchPaths[0],
			"/opt/clara/plugins",
		)
	}
}

func TestLoadWithConfD_DeepMerge(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "config.yaml")
	confD := filepath.Join(dir, "conf.d")
	if err := os.MkdirAll(confD, 0o750); err != nil {
		t.Fatal(err)
	}

	baseYaml := `
log_level: info
data_dir: /var/clara
integrations:
  db:
    path: /var/clara/data.db
mcp_servers:
  - name: github
    command: github-mcp-server stdio
    env:
      TOKEN: original-token
triggers:
  - id: t1
    name: Base Trigger 1
    type: event
    action:
      exec: lua base.lua
trigger_dirs:
  - /var/clara/triggers
`
	if err := os.WriteFile(basePath, []byte(baseYaml), 0o600); err != nil {
		t.Fatal(err)
	}

	llmYaml := `
log_level: debug
integrations:
  llm:
    providers:
      ollama:
        base_url: http://localhost:11434
`
	if err := os.WriteFile(filepath.Join(confD, "10-llm.yaml"), []byte(llmYaml), 0o600); err != nil {
		t.Fatal(err)
	}

	mcpAndTriggersYaml := `
mcp_servers:
  - name: github
    description: GitHub Tools
  - name: memory
    command: mcp-memory-server stdio
triggers:
  - id: t1
    name: Updated Trigger 1
  - id: t2
    name: New Trigger 2
    type: schedule
    action:
      exec: lua cron.lua
trigger_dirs:
  - /opt/clara/custom_triggers
`
	if err := os.WriteFile(filepath.Join(confD, "20-mcp-triggers.yaml"), []byte(mcpAndTriggersYaml), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.LoadWithConfD(basePath, confD)
	if err != nil {
		t.Fatalf("LoadWithConfD failed: %v", err)
	}

	// 1. Scalar override: log_level should be overridden by 10-llm.yaml
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel: got %q want %q", cfg.LogLevel, "debug")
	}
	if cfg.DataDir != "/var/clara" {
		t.Errorf("DataDir: got %q want %q", cfg.DataDir, "/var/clara")
	}

	// 2. Map deep merge: integrations should contain both db and llm
	if len(cfg.Integrations) != 2 {
		t.Fatalf("Integrations len: got %d want 2", len(cfg.Integrations))
	}
	if cfg.Integrations["db"]["path"] != "/var/clara/data.db" {
		t.Errorf("db.path: got %v", cfg.Integrations["db"]["path"])
	}
	if cfg.Integrations["llm"] == nil {
		t.Errorf("expected llm integration to be present")
	}

	// 3. Identity merge for mcp_servers (by name):
	// github should have merged description while preserving command and env; memory should be appended
	if len(cfg.MCPServers) != 2 {
		t.Fatalf("MCPServers len: got %d want 2", len(cfg.MCPServers))
	}
	if cfg.MCPServers[0].Name != "github" || cfg.MCPServers[0].Command != "github-mcp-server stdio" ||
		cfg.MCPServers[0].Description != "GitHub Tools" || cfg.MCPServers[0].Env["TOKEN"] != "original-token" {
		t.Errorf("MCPServers[0] not properly merged: %+v", cfg.MCPServers[0])
	}
	if cfg.MCPServers[1].Name != "memory" || cfg.MCPServers[1].Command != "mcp-memory-server stdio" {
		t.Errorf("MCPServers[1] not properly appended: %+v", cfg.MCPServers[1])
	}

	// 4. Identity merge for triggers (by id):
	// t1 should have updated name while keeping type and action; t2 should be appended
	if len(cfg.Triggers) != 2 {
		t.Fatalf("Triggers len: got %d want 2", len(cfg.Triggers))
	}
	if cfg.Triggers[0].ID != "t1" || cfg.Triggers[0].Name != "Updated Trigger 1" ||
		cfg.Triggers[0].Action.Exec != "lua base.lua" {
		t.Errorf("Triggers[0] not properly merged: %+v", cfg.Triggers[0])
	}
	if cfg.Triggers[1].ID != "t2" || cfg.Triggers[1].Name != "New Trigger 2" {
		t.Errorf("Triggers[1] not properly appended: %+v", cfg.Triggers[1])
	}

	// 5. Primitive slice append for trigger_dirs:
	if len(cfg.TriggerDirsOverride) != 2 {
		t.Fatalf("TriggerDirsOverride len: got %d want 2", len(cfg.TriggerDirsOverride))
	}
	if cfg.TriggerDirsOverride[0] != "/var/clara/triggers" ||
		cfg.TriggerDirsOverride[1] != "/opt/clara/custom_triggers" {
		t.Errorf("TriggerDirsOverride: got %v", cfg.TriggerDirsOverride)
	}
}

func TestLoad_ExplicitFileOnly(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "custom.yaml")
	confD := filepath.Join(dir, "conf.d")
	if err := os.MkdirAll(confD, 0o750); err != nil {
		t.Fatal(err)
	}

	customYaml := `log_level: warn`
	if err := os.WriteFile(basePath, []byte(customYaml), 0o600); err != nil {
		t.Fatal(err)
	}

	confDYaml := `log_level: debug`
	if err := os.WriteFile(filepath.Join(confD, "override.yaml"), []byte(confDYaml), 0o600); err != nil {
		t.Fatal(err)
	}

	// Load should only load custom.yaml and NOT merge conf.d
	cfg, err := config.Load(basePath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.LogLevel != "warn" {
		t.Errorf("LogLevel: got %q want %q", cfg.LogLevel, "warn")
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
