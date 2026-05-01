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
		t.Errorf("PluginSearchPaths[0]: got %q want %q", cfg.PluginSearchPaths[0], "/opt/clara/plugins")
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
