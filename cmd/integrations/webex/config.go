package main

import (
	"encoding/json"

	"github.com/cockroachdb/errors"
)

// Config is loaded from Clara's integrations.webex section in config.yaml.
type Config struct {
	EveURL  string `json:"eve_url"`
	Secret  string `json:"secret"`
	Machine string `json:"machine"`
}

func parseConfig(raw []byte) (Config, error) {
	var cfg Config
	if len(raw) == 0 {
		return cfg, errors.New("webex: no configuration provided")
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, errors.Wrap(err, "webex: unmarshal config")
	}
	if cfg.EveURL == "" {
		return cfg, errors.New("webex: eve_url is required")
	}
	if cfg.Secret == "" {
		return cfg, errors.New("webex: secret is required")
	}
	if cfg.Machine == "" {
		return cfg, errors.New("webex: machine is required")
	}
	return cfg, nil
}
