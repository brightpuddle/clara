// Package config provides configuration loading for Clara components.
package config

import (
	"os"
	"path/filepath"

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

// Load reads configuration from config.yaml in the data dir and environment.
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

	v.SetConfigName("config")
	v.SetConfigType("yaml")
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
