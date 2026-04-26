// Package config handles loading and parsing Clara's configuration file.
// Config is read from ~/.config/clara/config.yaml by default.
// All string values support ${ENV_VAR} expansion via os.ExpandEnv.
package config

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/google/shlex"
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

	// MCPCommandSearchPaths prepends additional search paths used to locate
	// bare MCP server commands and to build the PATH passed to subprocesses.
	MCPCommandSearchPaths []string `yaml:"mcp_command_search_paths"`

	// MCPServers lists the MCP servers the daemon manages.
	MCPServers []MCPServerConfig `yaml:"mcp_servers"`

	// Integrations configures the native Go plugins.
	Integrations map[string]map[string]any `yaml:"integrations"`

	// StdioMCP configures the local stdio MCP gateway server.
	StdioMCP *StdioMCPConfig `yaml:"stdio_mcp"`

	// LLM configures the built-in unified LLM MCP server.
	LLM *LLMConfig `yaml:"llm"`

	// MCPStartupTimeout is the maximum time to wait for MCP servers to be ready
	// before allowing intents to run.
	MCPStartupTimeout time.Duration `yaml:"mcp_startup_timeout"`

	// Testing overrides
	ControlSocketPathOverride    string `yaml:"-"`
	DynamicMCPSocketPathOverride string `yaml:"-"`
}

// StdioMCPConfig contains settings for exposing Clara tools/intents over a local stdio MCP server.
type StdioMCPConfig struct {
	ExposeTools   []string `yaml:"expose_tools"`
	ExposeIntents []string `yaml:"expose_intents"`
}

// LLMConfig contains settings for the built-in unified LLM MCP server.
type LLMConfig struct {
	Categories LLMCategories      `yaml:"categories"`
	Providers  LLMProvidersConfig `yaml:"providers"`
}

type LLMCategories struct {
	Fast       []LLMRoute `yaml:"fast"`
	Reasoning  []LLMRoute `yaml:"reasoning"`
	Local      []LLMRoute `yaml:"local"`
	Vision     []LLMRoute `yaml:"vision"`
	Embeddings []LLMRoute `yaml:"embeddings"`
}

type LLMRoute struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
}

type LLMProvidersConfig struct {
	Gemini *GeminiConfig `yaml:"gemini"`
	Ollama *OllamaConfig `yaml:"ollama"`
}

type GeminiConfig struct {
	APIKey  string `yaml:"api_key"`
	Model   string `yaml:"model"`
	BaseURL string `yaml:"base_url"`
}

type OllamaConfig struct {
	BaseURL string `yaml:"base_url"`
}

// MCPServerConfig describes a single MCP server managed by the Clara daemon.
// Either URL (streamable HTTP) or Command (stdio subprocess) must be provided;
// they are mutually exclusive.
type MCPServerConfig struct {
	// Name is the registry alias for this server (used in mcp://name/tool URIs).
	Name string `yaml:"name"`
	// URL is the base URL of a streamable HTTP MCP server (e.g.
	// "http://127.0.0.1:12306/mcp"). When set, Command and Env are
	// ignored; the server is reached over HTTP instead of as a subprocess.
	URL string `yaml:"url"`
	// Token is an optional Bearer token to send when connecting to an HTTP MCP server.
	Token string `yaml:"token"`
	// SkipVerify skips TLS certificate verification when connecting to an HTTP MCP server.
	SkipVerify bool `yaml:"skip_verify"`
	// Command is the full command string to run (stdio subprocess servers only).
	Command string `yaml:"command"`
	// Env injects additional environment variables into the subprocess.
	// Values support ${ENV_VAR} expansion.
	Env map[string]string `yaml:"env"`
	// Description is a human-readable summary of what this server provides.
	Description string `yaml:"description"`
}

// IsHTTPServer reports whether this config entry describes a streamable HTTP
// server (URL set) rather than a stdio subprocess (Command set).
func (s *MCPServerConfig) IsHTTPServer() bool {
	return s.URL != ""
}

// CommandArgs returns the command and its arguments as a slice of strings,
// split according to shell quoting rules.
func (s *MCPServerConfig) CommandArgs() ([]string, error) {
	if s.Command == "" {
		return nil, nil
	}
	args, err := shlex.Split(s.Command)
	if err != nil {
		return nil, errors.Wrapf(err, "split MCP command %q", s.Command)
	}
	return args, nil
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
	if cfg.MCPStartupTimeout == 0 {
		cfg.MCPStartupTimeout = 30 * time.Second
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

// MCPCommandSearchPathList returns the effective command search paths used to
// resolve bare MCP server commands and to build subprocess PATH values.
func (c *Config) MCPCommandSearchPathList() []string {
	paths := make([]string, 0, len(c.MCPCommandSearchPaths)+8)
	paths = append(paths, c.MCPCommandSearchPaths...)
	paths = append(paths, "/usr/local/bin", "/opt/homebrew/bin")
	paths = append(paths, filepath.SplitList(os.Getenv("PATH"))...)

	seen := make(map[string]struct{}, len(paths))
	effective := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		effective = append(effective, path)
	}
	return effective
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

// DynamicMCPSocketPath returns the absolute path to the daemon socket used for
// long-lived dynamic MCP peer attachments.
func (c *Config) DynamicMCPSocketPath() string {
	if c.DynamicMCPSocketPathOverride != "" {
		return c.DynamicMCPSocketPathOverride
	}
	return filepath.Join(c.DataDir, "clara-mcp.sock")
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

// LogLevelNormalized returns the log level string lowercased and trimmed.
func (c *Config) LogLevelNormalized() string {
	return strings.ToLower(strings.TrimSpace(c.LogLevel))
}
