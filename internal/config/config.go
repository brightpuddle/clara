// Package config handles loading and parsing Clara's configuration file.
// Config is read from ~/.config/clara/config.yaml by default.
// All string values support ${ENV_VAR} expansion via os.ExpandEnv.
package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
	"gopkg.in/yaml.v3"
)

// DefaultConfigPath returns the default config file path.
func DefaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "clara", "config.yaml")
}

// DefaultDataDir returns the default runtime data directory.
func DefaultDataDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "clara")
}

// Config is the top-level configuration for the Clara daemon.
type Config struct {
	// LogLevel controls the zerolog log level (trace, debug, info, warn, error).
	LogLevel string `yaml:"log_level"`

	// DataDir overrides the default runtime data directory.
	DataDir string `yaml:"data_dir"`

	// MCPServers lists the MCP servers the daemon manages.
	MCPServers []MCPServerConfig `yaml:"mcp_servers"`

	// Bridge configures the Swift native bridge subprocess.
	Bridge BridgeConfig `yaml:"bridge"`

	// LLM configures the language model used for Intent generation.
	LLM LLMConfig `yaml:"llm"`
}

// MCPServerConfig describes a single stdio-based MCP server subprocess.
type MCPServerConfig struct {
	// Name is the registry alias for this server (used in mcp://name/tool URIs).
	Name string `yaml:"name"`
	// Command is the executable to run.
	Command string `yaml:"command"`
	// Args are the command-line arguments passed to the subprocess.
	Args []string `yaml:"args"`
	// Env injects additional environment variables into the subprocess.
	// Values support ${ENV_VAR} expansion.
	Env map[string]string `yaml:"env"`
}

// BridgeConfig describes the Swift native bridge subprocess.
type BridgeConfig struct {
	// Path is the filesystem path to the ClaraBridge binary.
	Path string `yaml:"path"`
	// SocketPath overrides the default Unix Domain Socket path.
	SocketPath string `yaml:"socket_path"`
}

// LLMConfig configures the language model used for Markdown→Intent conversion.
type LLMConfig struct {
	// Provider is the MCP server name that exposes the LLM tool.
	Provider string `yaml:"provider"`
	// Model is the model identifier passed to the provider.
	Model string `yaml:"model"`
}

// Load reads and parses a config file at the given path.
// All string values are expanded with os.ExpandEnv.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrap(err, "read config file")
	}
	return parse(data)
}

// LoadDefault loads the config from the default path, creating the directory
// and an empty config if the file does not yet exist.
func LoadDefault() (*Config, error) {
	path := DefaultConfigPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return defaults(), nil
	}
	return Load(path)
}

func parse(data []byte) (*Config, error) {
	// Expand environment variables before YAML parsing so that ${VAR} in
	// any string value (including nested maps) is resolved at load time.
	expanded := os.ExpandEnv(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, errors.Wrap(err, "parse config yaml")
	}
	applyDefaults(&cfg)
	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	if cfg.DataDir == "" {
		cfg.DataDir = DefaultDataDir()
	}
	if cfg.Bridge.SocketPath == "" {
		cfg.Bridge.SocketPath = filepath.Join(cfg.DataDir, "bridge.sock")
	}
}

func defaults() *Config {
	cfg := &Config{}
	applyDefaults(cfg)
	return cfg
}

// expandEnvInMap recursively expands environment variables in all string values
// of a map. This is used for MCPServerConfig.Env after YAML parsing.
func expandEnvInMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = os.ExpandEnv(v)
	}
	return out
}

// ResolvedEnv returns a copy of the MCP server's Env map with all ${VAR}
// references expanded. Call this when building the subprocess environment.
func (s *MCPServerConfig) ResolvedEnv() map[string]string {
	return expandEnvInMap(s.Env)
}

// DBPath returns the absolute path to the SQLite database file.
func (c *Config) DBPath() string {
	return filepath.Join(c.DataDir, "clara.db")
}

// ControlSocketPath returns the absolute path to the daemon control socket.
func (c *Config) ControlSocketPath() string {
	return filepath.Join(c.DataDir, "clara.sock")
}

// TasksDir returns the directory where Markdown intent files are watched.
func (c *Config) TasksDir() string {
	return filepath.Join(c.DataDir, "tasks")
}

// LogPath returns the default daemon log file path.
func (c *Config) LogPath() string {
	return filepath.Join(c.DataDir, "clara.log")
}

// LogLevelNormalized returns the log level string lowercased and trimmed.
func (c *Config) LogLevelNormalized() string {
	return strings.ToLower(strings.TrimSpace(c.LogLevel))
}
