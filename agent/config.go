package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/brightpuddle/clara/internal/xdg"
	"gopkg.in/yaml.v3"
)

type agentConfig struct {
	Server struct {
		Addr string `yaml:"addr"`
	} `yaml:"server"`
	Notes struct {
		Dir     string `yaml:"dir"`
		Storage string `yaml:"storage"` // local | icloud — determines action_surface for proposals
	} `yaml:"notes"`
	AgentID      string `yaml:"agent_id"`
	PollInterval string `yaml:"poll_interval"`
}

func loadAgentConfig() agentConfig {
	cfg := defaultAgentConfig()

	path, err := xdg.ConfigFile("agent.yaml")
	if err != nil {
		slog.Warn("could not resolve XDG config path", "err", err)
	} else {
		if data, err := os.ReadFile(path); err == nil {
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				slog.Warn("could not parse agent config", "path", path, "err", err)
			} else {
				// Expand ~ in paths loaded from YAML.
				cfg.Notes.Dir = expandHomePath(cfg.Notes.Dir)
				slog.Info("loaded config", "path", path)
			}
		}
	}

	applyAgentEnvOverrides(&cfg)
	return cfg
}

func defaultAgentConfig() agentConfig {
	home, _ := os.UserHomeDir()
	var cfg agentConfig
	cfg.Server.Addr = "localhost:50051"
	cfg.Notes.Dir = home + "/notes"
	cfg.Notes.Storage = "local"
	cfg.AgentID = "default"
	cfg.PollInterval = "10s"
	return cfg
}

func applyAgentEnvOverrides(cfg *agentConfig) {
	if v := os.Getenv("CLARA_SERVER_ADDR"); v != "" {
		cfg.Server.Addr = v
	}
	if v := os.Getenv("CLARA_NOTES_DIR"); v != "" {
		cfg.Notes.Dir = expandHomePath(v)
	}
	if v := os.Getenv("CLARA_NOTES_STORAGE"); v != "" {
		cfg.Notes.Storage = v
	}
	if v := os.Getenv("CLARA_AGENT_ID"); v != "" {
		cfg.AgentID = v
	}
	if v := os.Getenv("CLARA_POLL_INTERVAL"); v != "" {
		cfg.PollInterval = v
	}
}

// expandHomePath expands a leading ~ to the user's home directory.
func expandHomePath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

func (c agentConfig) parsedPollInterval() time.Duration {
	d, err := time.ParseDuration(c.PollInterval)
	if err != nil {
		return 10 * time.Second
	}
	return d
}
