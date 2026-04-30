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

	// TasksDir overrides the default directory where .star intent files are watched.
	TasksDirOverride string `yaml:"tasks_dir"`

	// Integrations configures the native Go plugins.
	Integrations map[string]map[string]any `yaml:"integrations"`

	// Testing overrides
	ControlSocketPathOverride string `yaml:"-"`
}

// Save writes the config to the given path in YAML format.
func Save(path string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return errors.Wrap(err, "marshal config yaml")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return errors.Wrap(err, "create config directory")
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return errors.Wrap(err, "write config file")
	}
	return nil
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
}

func defaults() *Config {
	cfg := &Config{}
	applyDefaults(cfg)
	return cfg
}

// DBPath returns the absolute path to the SQLite database file used internally
// by the daemon for run-state persistence.
func (c *Config) DBPath() string {
	return filepath.Join(c.DataDir, "clara.db")
}

// ControlSocketPath returns the absolute path to the daemon control socket.
func (c *Config) ControlSocketPath() string {
	if c.ControlSocketPathOverride != "" {
		return c.ControlSocketPathOverride
	}
	return filepath.Join(c.DataDir, "clara.sock")
}

// TasksDir returns the directory where .star intent files are watched.
func (c *Config) TasksDir() string {
	if c.TasksDirOverride != "" {
		return c.TasksDirOverride
	}
	return filepath.Join(filepath.Dir(DefaultConfigPath()), "tasks")
}

// LogPath returns the default daemon log file path.
func (c *Config) LogPath() string {
	return filepath.Join(c.DataDir, "clara.log")
}

// IntentLogsDir returns the directory where per-intent JSONL log files are written.
func (c *Config) IntentLogsDir() string {
	return filepath.Join(c.DataDir, "logs")
}

// LogLevelNormalized returns the log level string lowercased and trimmed.
func (c *Config) LogLevelNormalized() string {
	return strings.ToLower(strings.TrimSpace(c.LogLevel))
}

