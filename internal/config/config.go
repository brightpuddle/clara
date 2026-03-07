// Package config provides configuration loading for Clara components.
package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Config holds the full Clara configuration.
type Config struct {
	// DataDir is the directory used for the SQLite database and Unix sockets.
	DataDir string `mapstructure:"data_dir"`

	// LogFile is the path to the log file. Empty means stderr.
	LogFile string `mapstructure:"log_file"`

	// LogLevel is one of: trace, debug, info, warn, error.
	LogLevel string `mapstructure:"log_level"`

	// Server holds configuration for the Clara server component.
	Server ServerConfig `mapstructure:"server"`

	// Agent holds configuration for the Clara agent component.
	Agent AgentConfig `mapstructure:"agent"`

	// Ollama holds Ollama inference configuration.
	Ollama OllamaConfig `mapstructure:"ollama"`

	// TUI holds terminal UI configuration.
	TUI TUIConfig `mapstructure:"tui"`
}

type ServerConfig struct {
	// Addr is the TCP address the server listens on.
	Addr string `mapstructure:"addr"`
}

type AgentConfig struct {
	// WatchDirs is the list of directories the agent monitors for new files.
	WatchDirs []string `mapstructure:"watch_dirs"`

	// IngestConcurrency controls the number of parallel ingestion workers.
	IngestConcurrency int `mapstructure:"ingest_concurrency"`
}

type OllamaConfig struct {
	// URL is the base URL of the Ollama API.
	URL string `mapstructure:"url"`

	// EmbedModel is the model used for generating embeddings.
	EmbedModel string `mapstructure:"embed_model"`
}

// TUIConfig holds theme and display settings for the terminal UI.
type TUIConfig struct {
	// ThemeMode controls which theme is active: "dark", "light", or "system".
	// When "system", dark-notify is used to follow macOS appearance.
	// If dark-notify is not installed and mode is "system", the dark theme is used.
	ThemeMode string `mapstructure:"theme_mode"`

	// DarkTheme is the bubbletint theme ID to use in dark mode.
	// Leave empty (or "native") to use the terminal's native 16 ANSI colors.
	DarkTheme string `mapstructure:"dark_theme"`

	// LightTheme is the bubbletint theme ID to use in light mode.
	// Leave empty (or "native") to use the terminal's native 16 ANSI colors.
	LightTheme string `mapstructure:"light_theme"`
}

// Load reads configuration from config.yaml and environment.
// Environment variables are prefixed with CLARA_ and override file values.
func Load() (*Config, error) {
	v := viper.New()

	// Defaults
	dataDir := defaultDataDir()
	v.SetDefault("data_dir", dataDir)
	v.SetDefault("log_level", "info")
	v.SetDefault("log_file", filepath.Join(dataDir, "logs", "clara.log"))
	v.SetDefault("server.addr", "localhost:50051")
	v.SetDefault("agent.ingest_concurrency", 4)
	v.SetDefault("ollama.url", "http://localhost:11434")
	v.SetDefault("ollama.embed_model", "nomic-embed-text")
	v.SetDefault("tui.theme_mode", "system")
	v.SetDefault("tui.dark_theme", "")
	v.SetDefault("tui.light_theme", "")

	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(defaultConfigDir())
	v.AddConfigPath(dataDir)
	v.AddConfigPath(".")

	v.SetEnvPrefix("CLARA")
	v.AutomaticEnv()

	// Ignore missing config file; use defaults + env vars.
	_ = v.ReadInConfig()

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// WriteDefaultConfig writes a commented default config.yaml to ~/.config/clara/
// if one does not already exist. It is safe to call on every startup.
func WriteDefaultConfig() error {
	cfgDir := defaultConfigDir()
	cfgPath := filepath.Join(cfgDir, "config.yaml")

	if _, err := os.Stat(cfgPath); err == nil {
		return nil // already exists
	}

	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		return err
	}

	dataDir := defaultDataDir()
	content := strings.Join([]string{
		"# Clara configuration",
		"# All values below show the built-in defaults.",
		"# Uncomment and modify any setting to override it.",
		"# Environment variables prefixed CLARA_ also override these values.",
		"",
		"# data_dir: " + dataDir,
		"# log_level: info",
		"# log_file: " + filepath.Join(dataDir, "logs", "clara.log"),
		"",
		"# server:",
		"#   addr: localhost:50051",
		"",
		"# agent:",
		"#   # Directories to watch for new/changed files.",
		"#   watch_dirs:",
		"#     - ~/Documents",
		"#     - ~/Notes",
		"#   ingest_concurrency: 4",
		"",
		"# ollama:",
		"#   url: http://localhost:11434",
		"#   embed_model: nomic-embed-text",
		"",
		"# tui:",
		"#   # Theme mode: dark, light, or system (follows macOS dark-notify if installed).",
		"#   theme_mode: system",
		"#   # Bubbletint theme ID for dark mode. Leave empty for native 16-color terminal theme.",
		"#   # See https://lrstanley.github.io/bubbletint/ for all available IDs.",
		"#   dark_theme: \"\"",
		"#   # Bubbletint theme ID for light mode. Leave empty for native 16-color terminal theme.",
		"#   light_theme: \"\"",
		"",
	}, "\n")

	return os.WriteFile(cfgPath, []byte(content), 0o644)
}

// AgentSocketPath returns the Unix socket path for the Agent gRPC server.
func (c *Config) AgentSocketPath() string {
	return filepath.Join(c.DataDir, "agent.sock")
}

// NativeSocketPath returns the Unix socket path for the Swift native worker.
func (c *Config) NativeSocketPath() string {
	return filepath.Join(c.DataDir, "native.sock")
}

// DBPath returns the path to the SQLite database file.
func (c *Config) DBPath() string {
	return filepath.Join(c.DataDir, "clara.db")
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".clara"
	}
	return filepath.Join(home, ".local", "share", "clara")
}

func defaultConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".config/clara"
	}
	return filepath.Join(home, ".config", "clara")
}
