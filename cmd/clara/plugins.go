package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/supervisor"
	"github.com/brightpuddle/clara/pkg/contract"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"github.com/rs/zerolog"
)

// pluginLoader manages the discovery and loading of native Go plugins.
type pluginLoader struct {
	reg *registry.Registry
	sup *supervisor.Supervisor
	log zerolog.Logger
}

func newPluginLoader(reg *registry.Registry, sup *supervisor.Supervisor, log zerolog.Logger) *pluginLoader {
	return &pluginLoader{
		reg: reg,
		sup: sup,
		log: log.With().Str("component", "plugin_loader").Logger(),
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
	intentsDir := filepath.Join(claraConfigDir, "intents")

	l.log.Debug().Str("integrations_dir", integrationsDir).Str("intents_dir", intentsDir).Msg("scanning for plugins")

	if err := l.loadIntegrations(integrationsDir); err != nil {
		l.log.Error().Err(err).Str("dir", integrationsDir).Msg("failed to load integrations")
	}

	if err := l.loadIntents(intentsDir); err != nil {
		l.log.Error().Err(err).Str("dir", intentsDir).Msg("failed to load intents")
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
		l.log.Info().Str("name", name).Str("path", path).Msg("discovered native integration")

		// For integrations, we need to launch the plugin and register its tools.
		// For the PoC, we'll just show how to inject the logger.
		_ = plugin.NewClient(&plugin.ClientConfig{
			HandshakeConfig: contract.HandshakeConfig,
			Plugins: map[string]plugin.Plugin{
				"shell": &contract.ShellIntegrationPlugin{},
			},
			Cmd:    exec.Command(path),
			Logger: buildHCLogAdapter(l.log, name),
		})
		// In a real implementation, we would keep the client, dispense the integration,
		// and wrap its methods as Clara tools in l.reg.
	}

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

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}

		path := filepath.Join(dir, name)
		l.log.Info().Str("name", name).Str("path", path).Msg("discovered native intent")

		// Register with supervisor as a native intent
		intent := &orchestrator.Intent{
			ID:           name,
			Description:  fmt.Sprintf("Native Go plugin intent: %s", name),
			WorkflowType: orchestrator.WorkflowTypeNative,
			Script:       path, // We store the path in Script field for native workflows
			Tasks: []orchestrator.Task{
				{
					Handler: "main",
					Mode:    orchestrator.IntentModeOnDemand,
				},
			},
		}

		if err := l.sup.RegisterIntent(path, intent); err != nil {
			l.log.Error().Err(err).Str("name", name).Msg("failed to register native intent")
		}
	}

	return nil
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

func (a *hcZerologAdapter) Trace(msg string, args ...interface{}) { a.Log(hclog.Trace, msg, args...) }
func (a *hcZerologAdapter) Debug(msg string, args ...interface{}) { a.Log(hclog.Debug, msg, args...) }
func (a *hcZerologAdapter) Info(msg string, args ...interface{})  { a.Log(hclog.Info, msg, args...) }
func (a *hcZerologAdapter) Warn(msg string, args ...interface{})  { a.Log(hclog.Warn, msg, args...) }
func (a *hcZerologAdapter) Error(msg string, args ...interface{}) { a.Log(hclog.Error, msg, args...) }

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
	return &hcZerologAdapter{log: a.log.With().Str("name", name).Logger(), name: a.name + "." + name, level: a.level}
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
