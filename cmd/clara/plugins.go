package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/brightpuddle/clara/internal/config"
	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/supervisor"
	"github.com/brightpuddle/clara/pkg/contract"
	"github.com/fsnotify/fsnotify"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"
)

// grpcPluginPaths lists known gRPC-based plugins and their candidate binary
// paths in order of preference. The first path that exists is used.
var grpcPluginPaths = map[string][]string{
	"macos": {
		"/usr/local/libexec/ClaraBridge.app/Contents/MacOS/ClaraBridge",
		"./build/ClaraBridge.app/Contents/MacOS/ClaraBridge",
	},
}

// resolveGRPCPluginPath returns the first existing binary path for a known
// gRPC plugin, or ("", false) if the plugin is unknown or not installed.
func resolveGRPCPluginPath(name string) (string, bool) {
	for _, p := range grpcPluginPaths[name] {
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	return "", false
}

// pluginLoader manages the discovery and loading of native Go plugins.
type pluginLoader struct {
	reg *registry.Registry
	sup *supervisor.Supervisor
	cfg *config.Config
	log zerolog.Logger

	mu      sync.Mutex
	clients map[string]*plugin.Client
}

func newPluginLoader(
	reg *registry.Registry,
	sup *supervisor.Supervisor,
	cfg *config.Config,
	log zerolog.Logger,
) *pluginLoader {
	return &pluginLoader{
		reg:     reg,
		sup:     sup,
		cfg:     cfg,
		log:     log.With().Str("component", "plugin_loader").Logger(),
		clients: make(map[string]*plugin.Client),
	}
}

func (l *pluginLoader) loadAll() error {
	l.log.Debug().Msg("plugin loader starting")
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	claraConfigDir := filepath.Join(home, ".config", "clara")
	integrationsDir := filepath.Join(claraConfigDir, "integrations")
	tasksDir := l.cfg.TasksDir()

	l.log.Debug().
		Str("integrations_dir", integrationsDir).
		Str("tasks_dir", tasksDir).
		Msg("scanning for plugins")

	if err := l.loadIntegrations(integrationsDir); err != nil {
		l.log.Error().
			Stack().
			Err(err).
			Str("dir", integrationsDir).
			Msg("failed to load integrations")
	}

	// Load gRPC plugins (e.g. ClaraBridge / macos) if present.
	for pluginName := range grpcPluginPaths {
		if p, ok := resolveGRPCPluginPath(pluginName); ok {
			if err := l.loadIntegrationAt(pluginName, p); err != nil {
				l.log.Error().Err(err).Str("name", pluginName).Msg("failed to load gRPC integration")
			}
		}
	}

	if err := l.loadIntents(tasksDir); err != nil {
		l.log.Error().Stack().Err(err).Str("dir", tasksDir).Msg("failed to load starlark tasks")
	}

	return nil
}

func (l *pluginLoader) loadIntegrations(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}

		path := filepath.Join(dir, name)
		if err := l.loadIntegrationAt(name, path); err != nil {
			l.log.Error().Stack().Err(err).Str("name", name).Msg("failed to load integration")
		}
	}

	return nil
}

// Load loads a specific plugin by name. It first checks the standard
// integrations directory, then falls back to known gRPC plugin paths.
func (l *pluginLoader) Load(name string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".config", "clara", "integrations", name)
	if _, err := os.Stat(path); err == nil {
		return l.loadIntegrationAt(name, path)
	}
	if grpcPath, ok := resolveGRPCPluginPath(name); ok {
		return l.loadIntegrationAt(name, grpcPath)
	}
	return fmt.Errorf("plugin %q not found", name)
}

// Unload kills a plugin client and unregisters its tools and intents.
func (l *pluginLoader) Unload(name string) error {
	l.mu.Lock()
	client, ok := l.clients[name]
	delete(l.clients, name)
	l.mu.Unlock()

	if !ok {
		return fmt.Errorf("plugin %q not loaded", name)
	}

	client.Kill()
	l.reg.UnregisterNamespace(name)
	_ = l.sup.UnregisterIntent(name)

	l.log.Info().Str("name", name).Msg("plugin unloaded")
	return nil
}

// Reload unloads and then loads a plugin.
func (l *pluginLoader) Reload(name string) error {
	if err := l.Unload(name); err != nil {
		l.log.Warn().Err(err).Str("name", name).Msg("unload failed during reload")
	}
	return l.Load(name)
}

// List returns a list of available plugins and their current status.
func (l *pluginLoader) List() []map[string]any {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dir := filepath.Join(home, ".config", "clara", "integrations")
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	seen := map[string]bool{}
	var result []map[string]any

	// Standard file-based plugins from the integrations directory.
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		name := entry.Name()
		seen[name] = true
		_, loaded := l.clients[name]
		status := "Unloaded"
		if loaded {
			status = "Loaded"
		}
		result = append(result, map[string]any{
			"name":   name,
			"status": status,
		})
	}

	// gRPC plugins (e.g. macos / ClaraBridge) — include if installed or loaded.
	for name := range grpcPluginPaths {
		if seen[name] {
			continue
		}
		_, loaded := l.clients[name]
		_, installed := resolveGRPCPluginPath(name)
		if !loaded && !installed {
			continue
		}
		status := "Unloaded"
		if loaded {
			status = "Loaded"
		}
		result = append(result, map[string]any{
			"name":   name,
			"status": status,
		})
	}

	return result
}

func (l *pluginLoader) loadIntegrationAt(name string, path string) error {
	l.mu.Lock()
	if _, ok := l.clients[name]; ok {
		l.mu.Unlock()
		return fmt.Errorf("plugin %q already loaded", name)
	}
	l.mu.Unlock()

	l.log.Info().Str("name", name).Str("path", path).Msg("loading native integration")

	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: contract.HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"chrome":      &contract.ChromeIntegrationPlugin{},
			"zk":          &contract.ZkIntegrationPlugin{},
			"llm":         &contract.LLMIntegrationPlugin{},
			"web":         &contract.WebIntegrationPlugin{},
			"macos":       &contract.IntegrationGRPCPlugin{},
			"tmux":        &contract.TmuxIntegrationPlugin{},
			"task": &contract.TaskIntegrationPlugin{},
		},
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolNetRPC, plugin.ProtocolGRPC},
		Cmd:              exec.Command(path),
		Logger:           buildHCLogAdapter(l.log, name),
	})

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return fmt.Errorf("failed to connect to integration plugin: %w", err)
	}

	raw, err := rpcClient.Dispense(name)
	if err != nil {
		client.Kill()
		return fmt.Errorf("failed to dispense integration plugin: %w", err)
	}

	integration, ok := raw.(contract.Integration)
	if !ok {
		client.Kill()
		return fmt.Errorf("plugin does not implement contract.Integration")
	}

	// Handle real-time events if supported
	if streamer, ok := integration.(contract.EventStreamer); ok {
		events, err := streamer.StreamEvents()
		if err == nil {
			go func() {
				for ev := range events {
					var params any
					if len(ev.Data) > 0 {
						_ = json.Unmarshal(ev.Data, &params)
					}
					l.reg.EmitNotification(name, ev.Name, params)
				}
			}()
		}
	}

	var configBytes []byte
	if l.cfg.Integrations != nil {
		if cfg, ok := l.cfg.Integrations[name]; ok {
			configBytes, err = json.Marshal(cfg)
			if err != nil {
				client.Kill()
				return fmt.Errorf("failed to marshal integration config: %w", err)
			}
		}
	}

	if err := integration.Configure(configBytes); err != nil {
		client.Kill()
		return fmt.Errorf("failed to configure integration: %w", err)
	}

	desc, err := integration.Description()
	if err == nil && desc != "" {
		l.reg.RegisterNamespaceDescription(name, desc)
	}

	toolsBytes, err := integration.Tools()
	if err != nil {
		client.Kill()
		return fmt.Errorf("failed to retrieve tools from integration: %w", err)
	}

	var tools []mcp.Tool
	if len(toolsBytes) > 0 {
		if err := json.Unmarshal(toolsBytes, &tools); err != nil {
			client.Kill()
			return fmt.Errorf("failed to decode tools from integration: %w", err)
		}
	}

	for _, tool := range tools {
		// Copy tool variable to avoid closure capture issues
		tool := tool
		originalToolName := tool.Name

		// Prefix the tool name with the integration name to namespace it
		tool.Name = l.reg.GetFQToolName(name, tool.Name)

		l.reg.RegisterWithSpec(tool, func(ctx context.Context, args map[string]any) (any, error) {
			argsBytes, err := json.Marshal(args)
			if err != nil {
				return nil, err
			}

			respBytes, err := integration.CallTool(originalToolName, argsBytes)
			if err != nil {
				return nil, err
			}

			var resp any
			if len(respBytes) > 0 {
				if err := json.Unmarshal(respBytes, &resp); err != nil {
					return string(respBytes), nil // Return as string if it isn't JSON
				}
			}
			return resp, nil
		})
	}

	l.mu.Lock()
	l.clients[name] = client
	l.mu.Unlock()

	l.log.Info().
		Str("name", name).
		Int("tools", len(tools)).
		Msg("successfully loaded native integration")
	return nil
}

func (l *pluginLoader) loadIntents(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	namespaces := l.reg.Namespaces()

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if strings.ToLower(filepath.Ext(name)) != ".star" {
			continue
		}

		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			l.log.Error().Err(err).Str("path", path).Msg("failed to read starlark intent file")
			continue
		}

		l.log.Info().Str("name", name).Str("path", path).Msg("loading starlark intent")

		intent, err := orchestrator.LoadIntentFile(path, data, namespaces)
		if err != nil {
			l.log.Error().Err(err).Str("path", path).Msg("failed to compile starlark intent")
			continue
		}

		if err := l.sup.RegisterIntent(path, intent); err != nil {
			l.log.Error().Err(err).Str("name", name).Msg("failed to register starlark intent")
		}
	}

	return nil
}

// reloadIntent reloads a single .star file, replacing any previously registered
// intent with the same ID. It is safe to call concurrently.
func (l *pluginLoader) reloadIntent(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		l.log.Error().Err(err).Str("path", path).Msg("failed to read starlark intent file")
		return
	}
	namespaces := l.reg.Namespaces()
	intent, err := orchestrator.LoadIntentFile(path, data, namespaces)
	if err != nil {
		l.log.Error().Err(err).Str("path", path).Msg("failed to compile starlark intent")
		return
	}
	if err := l.sup.RegisterIntent(path, intent); err != nil {
		l.log.Error().Err(err).Str("path", path).Msg("failed to register starlark intent")
		return
	}
	l.log.Info().Str("path", path).Msg("starlark intent reloaded")
}

// removeIntent unregisters the intent whose ID is derived from the given path.
func (l *pluginLoader) removeIntent(path string) {
	id := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if err := l.sup.UnregisterIntent(id); err != nil {
		l.log.Debug().Err(err).Str("id", id).Msg("could not unregister removed intent")
		return
	}
	l.log.Info().Str("id", id).Str("path", path).Msg("starlark intent removed")
}

// watchTasks starts an fsnotify watcher on dir and hot-reloads .star intents
// as files are created, modified, or deleted. It blocks until ctx is done.
func (l *pluginLoader) watchTasks(ctx context.Context, dir string) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		l.log.Error().Err(err).Str("dir", dir).Msg("failed to create tasks dir for watching")
		return
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		l.log.Error().Err(err).Msg("failed to create fsnotify watcher")
		return
	}
	defer watcher.Close()

	if err := watcher.Add(dir); err != nil {
		l.log.Error().Err(err).Str("dir", dir).Msg("failed to watch tasks dir")
		return
	}

	l.log.Info().Str("dir", dir).Msg("watching tasks directory for changes")

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			// Only care about .star files.
			if strings.ToLower(filepath.Ext(event.Name)) != ".star" {
				continue
			}
			switch {
			case event.Has(fsnotify.Create) || event.Has(fsnotify.Write):
				l.reloadIntent(event.Name)
			case event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename):
				l.removeIntent(event.Name)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			l.log.Error().Err(err).Msg("tasks dir watcher error")
		}
	}
}

// buildHCLogAdapter creates an hclog.Logger that redirects to Clara's zerolog.
func buildHCLogAdapter(log zerolog.Logger, name string) hclog.Logger {
	level := hclog.Info
	// Map zerolog level to hclog level
	switch log.GetLevel() {
	case zerolog.DebugLevel, zerolog.TraceLevel:
		level = hclog.Debug
	case zerolog.InfoLevel:
		level = hclog.Info
	case zerolog.WarnLevel:
		level = hclog.Warn
	case zerolog.ErrorLevel, zerolog.FatalLevel, zerolog.PanicLevel:
		level = hclog.Error
	}

	return &hcZerologAdapter{
		log:   log.With().Str("plugin", name).Logger(),
		name:  name,
		level: level,
	}
}

type hcZerologAdapter struct {
	log   zerolog.Logger
	name  string
	level hclog.Level
}

func (a *hcZerologAdapter) Log(level hclog.Level, msg string, args ...interface{}) {
	if level < a.level {
		return
	}
	var event *zerolog.Event
	switch level {
	case hclog.Debug, hclog.Trace:
		event = a.log.Debug()
	case hclog.Info:
		event = a.log.Info()
	case hclog.Warn:
		event = a.log.Warn()
	case hclog.Error:
		event = a.log.Error()
	default:
		event = a.log.Info()
	}

	for i := 0; i < len(args); i += 2 {
		key := fmt.Sprintf("%v", args[i])
		if i+1 < len(args) {
			event.Interface(key, args[i+1])
		}
	}
	event.Msg(msg)
}

func (a *hcZerologAdapter) Trace(
	msg string,
	args ...interface{},
) {
	a.Log(hclog.Trace, msg, args...)
}

func (a *hcZerologAdapter) Debug(
	msg string,
	args ...interface{},
) {
	a.Log(hclog.Debug, msg, args...)
}

func (a *hcZerologAdapter) Info(
	msg string,
	args ...interface{},
) {
	a.Log(hclog.Info, msg, args...)
}

func (a *hcZerologAdapter) Warn(
	msg string,
	args ...interface{},
) {
	a.Log(hclog.Warn, msg, args...)
}

func (a *hcZerologAdapter) Error(
	msg string,
	args ...interface{},
) {
	a.Log(hclog.Error, msg, args...)
}

func (a *hcZerologAdapter) IsTrace() bool { return a.level <= hclog.Trace }
func (a *hcZerologAdapter) IsDebug() bool { return a.level <= hclog.Debug }
func (a *hcZerologAdapter) IsInfo() bool  { return a.level <= hclog.Info }
func (a *hcZerologAdapter) IsWarn() bool  { return a.level <= hclog.Warn }
func (a *hcZerologAdapter) IsError() bool { return a.level <= hclog.Error }

func (a *hcZerologAdapter) With(args ...interface{}) hclog.Logger {
	newLog := a.log.With()
	for i := 0; i < len(args); i += 2 {
		key := fmt.Sprintf("%v", args[i])
		if i+1 < len(args) {
			newLog = newLog.Interface(key, args[i+1])
		}
	}
	return &hcZerologAdapter{log: newLog.Logger(), name: a.name, level: a.level}
}

func (a *hcZerologAdapter) Named(name string) hclog.Logger {
	return &hcZerologAdapter{
		log:   a.log.With().Str("name", name).Logger(),
		name:  a.name + "." + name,
		level: a.level,
	}
}

func (a *hcZerologAdapter) ResetNamed(name string) hclog.Logger {
	return a.Named(name)
}

func (a *hcZerologAdapter) SetLevel(level hclog.Level) {
	a.level = level
}

func (a *hcZerologAdapter) StandardLogger(opts *hclog.StandardLoggerOptions) *log.Logger {
	return nil // Not strictly needed for go-plugin
}

func (a *hcZerologAdapter) StandardWriter(opts *hclog.StandardLoggerOptions) io.Writer {
	return a.log // zerolog.Logger implements io.Writer
}

func (a *hcZerologAdapter) GetLevel() hclog.Level {
	return a.level
}

func (a *hcZerologAdapter) ImpliedArgs() []interface{} {
	return nil
}

func (a *hcZerologAdapter) Name() string {
	return a.name
}
