// Package config provides configuration loading for Clara components.
package config

import (
"encoding/json"
"os"
"path/filepath"
"strings"

"github.com/spf13/viper"
)

// Config holds the full Clara configuration.
type Config struct {
	DataDir          string             `mapstructure:"data_dir"`
	LogFile          string             `mapstructure:"log_file"`
	LogLevel         string             `mapstructure:"log_level"`
	NativeWorkerPath string             `mapstructure:"native_worker_path"`
	Server           ServerConfig       `mapstructure:"server"`
	Ollama           OllamaConfig       `mapstructure:"ollama"`
	TUI              TUIConfig          `mapstructure:"tui"`
	Integrations     IntegrationsConfig `mapstructure:"integrations"`
}

type ServerConfig struct {
Addr string `mapstructure:"addr"`
}

type IntegrationsConfig struct {
Filesystem  FilesystemConfig  `mapstructure:"filesystem"`
Reminders   RemindersConfig   `mapstructure:"reminders"`
Taskwarrior TaskwarriorConfig `mapstructure:"taskwarrior"`
}

type FilesystemConfig struct {
Enabled           bool     `mapstructure:"enabled"`
WatchDirs         []string `mapstructure:"watch_dirs"`
IngestConcurrency int      `mapstructure:"ingest_concurrency"`
}

type RemindersConfig struct {
Enabled bool `mapstructure:"enabled"`
}

type TaskwarriorConfig struct {
Enabled    bool   `mapstructure:"enabled"`
BinaryPath string `mapstructure:"binary_path"`
DataDir    string `mapstructure:"data_dir"`
}

type OllamaConfig struct {
URL        string `mapstructure:"url"`
EmbedModel string `mapstructure:"embed_model"`
}

type TUIConfig struct {
ThemeMode  string `mapstructure:"theme_mode"`
DarkTheme  string `mapstructure:"dark_theme"`
LightTheme string `mapstructure:"light_theme"`
}

// Load reads configuration from config.yaml and environment.
func Load() (*Config, error) {
v := viper.New()

dataDir := defaultDataDir()
home, _ := os.UserHomeDir()

v.SetDefault("data_dir", dataDir)
v.SetDefault("log_level", "info")
v.SetDefault("log_file", filepath.Join(dataDir, "logs", "clara.log"))
v.SetDefault("native_worker_path", "")
v.SetDefault("server.addr", "localhost:50051")

v.SetDefault("ollama.url", "http://localhost:11434")
v.SetDefault("ollama.embed_model", "nomic-embed-text")
v.SetDefault("tui.theme_mode", "system")
v.SetDefault("tui.dark_theme", "")
v.SetDefault("tui.light_theme", "")
v.SetDefault("integrations.filesystem.enabled", true)
v.SetDefault("integrations.filesystem.ingest_concurrency", 4)
v.SetDefault("integrations.reminders.enabled", true)
v.SetDefault("integrations.taskwarrior.enabled", true)
v.SetDefault("integrations.taskwarrior.binary_path", "task")

v.SetConfigName("config")
v.SetConfigType("yaml")
v.AddConfigPath(defaultConfigDir())
v.AddConfigPath(dataDir)
v.AddConfigPath(".")

v.SetEnvPrefix("CLARA")
v.AutomaticEnv()

_ = v.ReadInConfig()

cfg := &Config{}
if err := v.Unmarshal(cfg); err != nil {
return nil, err
}

// Backward compatibility: copy legacy agent.watch_dirs if new field is empty.
if legacy := v.GetStringSlice("agent.watch_dirs"); len(legacy) > 0 && len(cfg.Integrations.Filesystem.WatchDirs) == 0 {
cfg.Integrations.Filesystem.WatchDirs = legacy
}

// Set taskwarrior data_dir default after unmarshal.
if cfg.Integrations.Taskwarrior.DataDir == "" && home != "" {
cfg.Integrations.Taskwarrior.DataDir = filepath.Join(home, ".task")
}

// Expand ~ in path fields.
cfg.DataDir = expandHome(cfg.DataDir)
cfg.LogFile = expandHome(cfg.LogFile)
cfg.NativeWorkerPath = expandHome(cfg.NativeWorkerPath)
for i, d := range cfg.Integrations.Filesystem.WatchDirs {

cfg.Integrations.Filesystem.WatchDirs[i] = expandHome(d)
}
cfg.Integrations.Taskwarrior.DataDir = expandHome(cfg.Integrations.Taskwarrior.DataDir)

return cfg, nil
}

// expandHome replaces a leading "~/" with the user's home directory.
func expandHome(path string) string {
if path == "~" {
home, err := os.UserHomeDir()
if err != nil {
return path
}
return home
}
if strings.HasPrefix(path, "~/") {
home, err := os.UserHomeDir()
if err != nil {
return path
}
return filepath.Join(home, path[2:])
}
return path
}

// WriteDefaultConfig writes a commented default config.yaml to ~/.config/clara/
// if one does not already exist.
func WriteDefaultConfig() error {
cfgDir := defaultConfigDir()
cfgPath := filepath.Join(cfgDir, "config.yaml")

if _, err := os.Stat(cfgPath); err == nil {
return nil
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
"# integrations:",
"#   filesystem:",
"#     enabled: true",
"#     watch_dirs:",
"#       - ~/Documents",
"#       - ~/Notes",
"#     ingest_concurrency: 4",
"#   reminders:",
"#     enabled: true",
"#   taskwarrior:",
"#     enabled: true",
"#     binary_path: task",
"#     data_dir: ~/.task",
"",
"# ollama:",
"#   url: http://localhost:11434",
"#   embed_model: nomic-embed-text",
"",
"# tui:",
"#   theme_mode: system",
"#   dark_theme: \"\"",
"#   light_theme: \"\"",
"",
}, "\n")

if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
return err
}

return writeSchemaFile(cfgDir)
}

// SchemaPath returns the path to the JSON schema for config.yaml.
func SchemaPath() string {
return filepath.Join(defaultConfigDir(), "config.schema.json")
}

// ConfigPath returns the path to the config.yaml file.
func ConfigPath() string {
return filepath.Join(defaultConfigDir(), "config.yaml")
}

func writeSchemaFile(cfgDir string) error {
schemaPath := filepath.Join(cfgDir, "config.schema.json")
if _, err := os.Stat(schemaPath); err == nil {
return nil
}
schema := configSchema()
data, err := json.MarshalIndent(schema, "", "  ")
if err != nil {
return err
}
return os.WriteFile(schemaPath, data, 0o644)
}

func (c *Config) AgentSocketPath() string {
return filepath.Join(c.DataDir, "agent.sock")
}

func (c *Config) NativeSocketPath() string {
return filepath.Join(c.DataDir, "native.sock")
}

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
