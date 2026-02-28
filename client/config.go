package main

import (
	"log/slog"
	"os"

	"github.com/brightpuddle/clara/internal/xdg"
	"gopkg.in/yaml.v3"
)

type clientConfig struct {
	Server struct {
		URL string `yaml:"url"`
	} `yaml:"server"`
}

func loadClientConfig() clientConfig {
	cfg := defaultClientConfig()

	path, err := xdg.ConfigFile("client.yaml")
	if err != nil {
		slog.Warn("could not resolve XDG config path", "err", err)
	} else {
		if data, err := os.ReadFile(path); err == nil {
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				slog.Warn("could not parse client config", "path", path, "err", err)
			}
		}
	}

	applyClientEnvOverrides(&cfg)
	return cfg
}

func defaultClientConfig() clientConfig {
	var cfg clientConfig
	cfg.Server.URL = "http://localhost:8080"
	return cfg
}

func applyClientEnvOverrides(cfg *clientConfig) {
	if v := os.Getenv("CLARA_SERVER_URL"); v != "" {
		cfg.Server.URL = v
	}
}
