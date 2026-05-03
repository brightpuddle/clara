package main

import (
	"encoding/json"

	"github.com/cockroachdb/errors"
)

// Config is loaded from Clara's integrations.discord section in config.yaml.
type Config struct {
	EveURL  string `json:"eve_url"`
	Secret  string `json:"secret"`
	Machine string `json:"machine"`
}

func parseConfig(raw []byte) (Config, error) {
	var cfg Config
	if len(raw) == 0 {
		return cfg, errors.New("discord: no configuration provided")
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, errors.Wrap(err, "discord: unmarshal config")
	}
	if cfg.EveURL == "" {
		return cfg, errors.New("discord: eve_url is required")
	}
	if cfg.Secret == "" {
		return cfg, errors.New("discord: secret is required")
	}
	if cfg.Machine == "" {
		return cfg, errors.New("discord: machine is required")
	}
	return cfg, nil
}
