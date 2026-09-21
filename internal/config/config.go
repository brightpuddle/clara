// Package config handles loading and parsing Clara's configuration file.
// Config is read from ~/.config/clara/config.yaml by default.
// All string values support ${ENV_VAR} expansion via os.ExpandEnv.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/google/shlex"
	"gopkg.in/yaml.v3"

	"github.com/brightpuddle/clara/internal/trigger"
)

// DefaultConfigPath returns the default config file path.
func DefaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "clara", "config.yaml")
}

// DefaultConfDDir returns the default conf.d directory path.
func DefaultConfDDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "clara", "conf.d")
}

// DefaultDataDir returns the default runtime data directory.
func DefaultDataDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "clara")
}

// PluginConfig describes a single entry in the plugin startup whitelist.
type PluginConfig struct {
	// Name is the registry namespace key and the binary filename used when
	// scanning PluginSearchPaths.
	Name string `yaml:"name"`
	// Path is an optional explicit binary path. When set, PluginSearchPaths
	// are not consulted. Use this for non-standard locations such as macOS
	// app bundles (e.g. ClaraBridge).
	Path string `yaml:"path"`
}

// Config is the top-level configuration for the Clara daemon.
type Config struct {
	// LogLevel controls the zerolog log level (trace, debug, info, warn, error).
	LogLevel string `yaml:"log_level"`

	// DataDir overrides the default runtime data directory.
	DataDir string `yaml:"data_dir"`

	// BuilderRepoRoot is the path to the Clara source tree. The builder uses
	// this to write a `replace github.com/brightpuddle/clara => <path>`
	// directive into sandbox go.mod files so generated actuators can import
	// pkg/sdk without a published module version. If empty, the builder falls
	// back to the CLARA_REPO_ROOT environment variable, then walks up from the
	// running binary's directory.
	BuilderRepoRoot string `yaml:"builder_repo_root"`

	// Triggers is the list of trigger definitions configured inline in config.yaml.
	Triggers []trigger.Definition `yaml:"triggers"`

	// TriggerDirs overrides the default directories where trigger .yaml files are loaded.
	// When non-empty, the default (~/.config/clara/triggers) is not used unless explicitly listed.
	TriggerDirsOverride []string `yaml:"trigger_dirs"`

	// TaskDirs overrides the default directories where .star intent files are watched.
	// When non-empty, the default (~/.config/clara/tasks) is not used unless
	// explicitly listed. Paths are searched in order; if the same intent ID
	// appears in more than one directory, the first occurrence wins and
	// subsequent ones are logged as errors.
	TaskDirsOverride []string `yaml:"task_dirs"`

	// Plugins is the ordered whitelist of plugins to load on daemon startup.
	// When absent, all binaries found in PluginSearchPaths[0] are loaded
	// (backward-compatible behaviour).
	Plugins []PluginConfig `yaml:"plugins"`

	// PluginSearchPaths is the ordered list of directories searched when
	// resolving a plugin binary by name. Defaults to the standard integrations
	// directory followed by /usr/local/libexec.
	PluginSearchPaths []string `yaml:"plugin_search_paths"`

	// Integrations configures the native Go plugins.
	Integrations map[string]map[string]any `yaml:"integrations"`

	// MCPCommandSearchPaths prepends additional search paths used to locate
	// bare MCP server commands and to build the PATH passed to subprocesses.
	MCPCommandSearchPaths []string `yaml:"mcp_command_search_paths"`

	// MCPServers lists the MCP servers the daemon manages.
	MCPServers []MCPServerConfig `yaml:"mcp_servers"`

	// MCPStartupTimeout is the maximum time to wait for MCP servers to be ready
	// before allowing intents to run.
	MCPStartupTimeout time.Duration `yaml:"mcp_startup_timeout"`

	// Notify configures the notification backend.
	Notify NotifyConfig `yaml:"notify"`

	// Server configures the Clara HTTP server for remote MCP and webhooks.
	Server ServerConfig `yaml:"server"`

	// MCPExposeProfiles defines named, whitelist-only subsets of tools that
	// `clara mcp serve --profile <name>` may expose to an external MCP client.
	MCPExposeProfiles map[string]MCPExposeProfile `yaml:"mcp_expose_profiles"`

	// Testing overrides
	ControlSocketPathOverride string `yaml:"-"`
}

// ServerConfig configures the Clara HTTP server for remote MCP and webhooks.
type ServerConfig struct {
	// ListenAddr is the address to bind the HTTP server to (e.g., ":4444").
	// If empty, the HTTP server is disabled.
	ListenAddr string `yaml:"listen_addr"`

	// SharedSecret is the bearer token expected from remote Clara instances.
	SharedSecret string `yaml:"shared_secret"`
}

// MCPServerConfig describes a single MCP server managed by the Clara daemon.
// Either URL (streamable HTTP) or Command (stdio subprocess) must be provided;
// they are mutually exclusive.
type MCPServerConfig struct {
	// Name is the registry alias for this server.
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

// ResolvedEnv returns a copy of the MCP server's Env map with all ${VAR}
// references expanded.
func (s *MCPServerConfig) ResolvedEnv() map[string]string {
	if s.Env == nil {
		return nil
	}
	out := make(map[string]string, len(s.Env))
	for k, v := range s.Env {
		out[k] = os.ExpandEnv(v)
	}
	return out
}

// NotifyConfig configures the notification backend for notify.send and notify.ask.
type NotifyConfig struct {
	// Backend selects the delivery mechanism: dummy (default), macos, webex, discord.
	Backend string `yaml:"backend"`

	// Webex configuration (used when backend = "webex").
	Webex WebexNotifyConfig `yaml:"webex"`

	// Discord configuration (used when backend = "discord").
	Discord DiscordNotifyConfig `yaml:"discord"`
}

// WebexNotifyConfig holds credentials for the Webex notification backend.
type WebexNotifyConfig struct {
	RoomID string `yaml:"room_id"`
}

// DiscordNotifyConfig configures the Discord notification backend.
// Notifications are routed through the eve relay (not directly to Discord),
// so no bot token is needed here — that lives in integrations.discord.
type DiscordNotifyConfig struct {
	ChannelID string `yaml:"channel_id"`
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

// LoadDefault loads the config from the default path and conf.d directory,
// creating the directory and an empty config if the file does not yet exist.
func LoadDefault() (*Config, error) {
	return LoadWithConfD(DefaultConfigPath(), DefaultConfDDir())
}

// LoadWithConfD loads the base config file at basePath (if it exists) and deep merges
// all *.yaml and *.yml files found non-recursively in confDDir in lexical order.
func LoadWithConfD(basePath, confDDir string) (*Config, error) {
	var mergedMap map[string]any

	// 1. Read base config if it exists
	if _, err := os.Stat(basePath); err == nil {
		data, err := os.ReadFile(basePath)
		if err != nil {
			return nil, errors.Wrapf(err, "read base config %q", basePath)
		}
		expanded := os.ExpandEnv(string(data))
		var baseMap map[string]any
		if err := yaml.Unmarshal([]byte(expanded), &baseMap); err != nil {
			return nil, errors.Wrapf(err, "parse base config %q", basePath)
		}
		mergedMap = baseMap
	}

	if mergedMap == nil {
		mergedMap = make(map[string]any)
	}

	// 2. Read and merge conf.d files in lexical order
	if confDDir != "" {
		entries, err := os.ReadDir(confDDir)
		if err != nil && !os.IsNotExist(err) {
			return nil, errors.Wrapf(err, "read conf.d directory %q", confDDir)
		}

		var files []string
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			if ext == ".yaml" || ext == ".yml" {
				files = append(files, filepath.Join(confDDir, entry.Name()))
			}
		}

		sort.Strings(files)

		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				return nil, errors.Wrapf(err, "read conf.d file %q", file)
			}
			expanded := os.ExpandEnv(string(data))
			var overrideMap map[string]any
			if err := yaml.Unmarshal([]byte(expanded), &overrideMap); err != nil {
				return nil, errors.Wrapf(err, "parse conf.d file %q", file)
			}
			if overrideMap != nil {
				mergedMap = mergeMaps(mergedMap, overrideMap)
			}
		}
	}

	// If neither base nor conf.d files produced any config, return defaults.
	if len(mergedMap) == 0 {
		return defaults(), nil
	}

	// Marshal merged map back to YAML and unmarshal into Config struct
	mergedBytes, err := yaml.Marshal(mergedMap)
	if err != nil {
		return nil, errors.Wrap(err, "marshal merged config")
	}

	var cfg Config
	if err := yaml.Unmarshal(mergedBytes, &cfg); err != nil {
		return nil, errors.Wrap(err, "unmarshal merged config")
	}

	applyDefaults(&cfg)
	return &cfg, nil
}

func mergeMaps(base, override map[string]any) map[string]any {
	result := make(map[string]any, len(base))
	for k, v := range base {
		result[k] = v
	}

	for k, overrideVal := range override {
		baseVal, exists := result[k]
		if !exists {
			result[k] = overrideVal
			continue
		}

		// If both are maps, recursively merge them
		baseMap, baseIsMap := toStringMap(baseVal)
		overrideMap, overrideIsMap := toStringMap(overrideVal)
		if baseIsMap && overrideIsMap {
			result[k] = mergeMaps(baseMap, overrideMap)
			continue
		}

		// If both are slices, merge with identity or fallback to append
		baseSlice, baseIsSlice := toSlice(baseVal)
		overrideSlice, overrideIsSlice := toSlice(overrideVal)
		if baseIsSlice && overrideIsSlice {
			result[k] = mergeSlices(baseSlice, overrideSlice)
			continue
		}

		// Otherwise, override scalar / mismatched value
		result[k] = overrideVal
	}

	return result
}

func mergeSlices(base, override []any) []any {
	// Check if either slice contains elements with identity keys ("id" or "name")
	hasIdentity := false
	for _, item := range override {
		if _, ok := getElementIdentity(item); ok {
			hasIdentity = true
			break
		}
	}
	if !hasIdentity {
		for _, item := range base {
			if _, ok := getElementIdentity(item); ok {
				hasIdentity = true
				break
			}
		}
	}

	// Fallback to append if no identity key is found
	if !hasIdentity {
		merged := make([]any, 0, len(base)+len(override))
		merged = append(merged, base...)
		merged = append(merged, override...)
		return merged
	}

	// Identity-based merge
	result := make([]any, len(base))
	copy(result, base)

	for _, item := range override {
		id, ok := getElementIdentity(item)
		if !ok {
			result = append(result, item)
			continue
		}

		foundIdx := -1
		for i, baseItem := range result {
			baseID, baseOK := getElementIdentity(baseItem)
			if baseOK && baseID == id {
				foundIdx = i
				break
			}
		}

		if foundIdx >= 0 {
			baseMap, baseMapOK := toStringMap(result[foundIdx])
			overrideMap, overrideMapOK := toStringMap(item)
			if baseMapOK && overrideMapOK {
				result[foundIdx] = mergeMaps(baseMap, overrideMap)
			} else {
				result[foundIdx] = item
			}
		} else {
			result = append(result, item)
		}
	}

	return result
}

func getElementIdentity(elem any) (string, bool) {
	m, ok := toStringMap(elem)
	if !ok {
		return "", false
	}
	if id, ok := m["id"].(string); ok && id != "" {
		return id, true
	}
	if name, ok := m["name"].(string); ok && name != "" {
		return name, true
	}
	return "", false
}

func toStringMap(v any) (map[string]any, bool) {
	if m, ok := v.(map[string]any); ok {
		return m, true
	}
	if m, ok := v.(map[any]any); ok {
		res := make(map[string]any, len(m))
		for k, val := range m {
			res[fmt.Sprintf("%v", k)] = val
		}
		return res, true
	}
	return nil, false
}

func toSlice(v any) ([]any, bool) {
	if s, ok := v.([]any); ok {
		return s, true
	}
	return nil, false
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
	if len(cfg.PluginSearchPaths) == 0 {
		home, _ := os.UserHomeDir()
		cfg.PluginSearchPaths = []string{
			filepath.Join(home, ".config", "clara", "integrations"),
			"/usr/local/libexec",
		}
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

// expandTilde replaces a leading ~ with the user's home directory.
func expandTilde(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return home + path[1:]
	}
	return path
}

// TriggerDirs returns the directories where trigger .yaml files are loaded.
// Leading tildes in each path are expanded to the user's home directory.
func (c *Config) TriggerDirs() []string {
	if len(c.TriggerDirsOverride) > 0 {
		expanded := make([]string, len(c.TriggerDirsOverride))
		for i, d := range c.TriggerDirsOverride {
			expanded[i] = expandTilde(d)
		}
		return expanded
	}
	return []string{filepath.Join(filepath.Dir(DefaultConfigPath()), "triggers")}
}

// TaskDirs returns the directories where .star intent files are watched.
// Leading tildes in each path are expanded to the user's home directory.
func (c *Config) TaskDirs() []string {
	if len(c.TaskDirsOverride) > 0 {
		expanded := make([]string, len(c.TaskDirsOverride))
		for i, d := range c.TaskDirsOverride {
			expanded[i] = expandTilde(d)
		}
		return expanded
	}
	return []string{filepath.Join(filepath.Dir(DefaultConfigPath()), "tasks")}
}

// LogPath returns the default daemon log file path.
func (c *Config) LogPath() string {
	return filepath.Join(c.DataDir, "clara.log")
}

// IntentLogsDir returns the directory where per-intent JSONL log files are written.
func (c *Config) IntentLogsDir() string {
	return filepath.Join(c.DataDir, "logs")
}

// SDKDir returns the directory where auto-generated TypeScript stubs and SDK files live.
func (c *Config) SDKDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "clara", "sdk")
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

// LogLevelNormalized returns the log level string lowercased and trimmed.
func (c *Config) LogLevelNormalized() string {
	return strings.ToLower(strings.TrimSpace(c.LogLevel))
}
