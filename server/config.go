package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/brightpuddle/clara/internal/xdg"
	"github.com/brightpuddle/clara/server/rag"
	"gopkg.in/yaml.v3"
)

// serverConfig is the full server configuration.
// Precedence (highest to lowest): env vars → config file → built-in defaults.
type serverConfig struct {
	DB struct {
		DSN string `yaml:"dsn"`
	} `yaml:"db"`
	AI       aiConfig `yaml:"ai"`
	Temporal struct {
		Host string `yaml:"host"`
	} `yaml:"temporal"`
	GRPC struct {
		Addr string `yaml:"addr"`
	} `yaml:"grpc"`
	HTTP struct {
		Addr string `yaml:"addr"`
	} `yaml:"http"`
}

type aiConfig struct {
	// Provider selects the embedding backend: "ollama" (default) or "openai".
	Provider string      `yaml:"provider"`
	Ollama   ollamaAI    `yaml:"ollama"`
	OpenAI   openAIConfig `yaml:"openai"`
}

type ollamaAI struct {
	BaseURL    string `yaml:"base_url"`
	EmbedModel string `yaml:"embed_model"`
}

type openAIConfig struct {
	APIKey     string `yaml:"api_key"`
	EmbedModel string `yaml:"embed_model"`
	// BaseURL can be overridden to point to a compatible API (e.g. Azure OpenAI,
	// local vLLM, Ollama's OpenAI-compatible endpoint).
	BaseURL string `yaml:"base_url"`
}

func loadServerConfig() serverConfig {
	cfg := defaultServerConfig()

	path, err := xdg.ConfigFile("server.yaml")
	if err != nil {
		slog.Warn("could not resolve XDG config path", "err", err)
	} else {
		if data, err := os.ReadFile(path); err == nil {
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				slog.Warn("could not parse server config", "path", path, "err", err)
			} else {
				slog.Info("loaded config", "path", path)
			}
		}
		// Missing config file is not an error — defaults are used.
	}

	applyServerEnvOverrides(&cfg)
	return cfg
}

func defaultServerConfig() serverConfig {
	var cfg serverConfig
	cfg.DB.DSN = "postgres://clara:clara@localhost:5432/clara?sslmode=disable"
	cfg.AI.Provider = "ollama"
	cfg.AI.Ollama.BaseURL = "http://localhost:11434"
	cfg.AI.Ollama.EmbedModel = "nomic-embed-text"
	cfg.AI.OpenAI.EmbedModel = "text-embedding-3-small"
	cfg.Temporal.Host = "localhost:7233"
	cfg.GRPC.Addr = ":50051"
	cfg.HTTP.Addr = ":8080"
	return cfg
}

// applyServerEnvOverrides overwrites config fields with env vars when set.
func applyServerEnvOverrides(cfg *serverConfig) {
	if v := os.Getenv("CLARA_DB_DSN"); v != "" {
		cfg.DB.DSN = v
	}
	if v := os.Getenv("CLARA_AI_PROVIDER"); v != "" {
		cfg.AI.Provider = v
	}
	if v := os.Getenv("CLARA_OLLAMA_URL"); v != "" {
		cfg.AI.Ollama.BaseURL = v
	}
	if v := os.Getenv("CLARA_OLLAMA_EMBED_MODEL"); v != "" {
		cfg.AI.Ollama.EmbedModel = v
	}
	if v := os.Getenv("CLARA_OPENAI_API_KEY"); v != "" {
		cfg.AI.OpenAI.APIKey = v
	}
	if v := os.Getenv("CLARA_OPENAI_EMBED_MODEL"); v != "" {
		cfg.AI.OpenAI.EmbedModel = v
	}
	if v := os.Getenv("CLARA_OPENAI_BASE_URL"); v != "" {
		cfg.AI.OpenAI.BaseURL = v
	}
	if v := os.Getenv("CLARA_TEMPORAL_HOST"); v != "" {
		cfg.Temporal.Host = v
	}
	if v := os.Getenv("CLARA_GRPC_ADDR"); v != "" {
		cfg.GRPC.Addr = v
	}
	if v := os.Getenv("CLARA_HTTP_ADDR"); v != "" {
		cfg.HTTP.Addr = v
	}
}

// buildEmbedder constructs the configured Embedder implementation.
func buildEmbedder(cfg aiConfig) (rag.Embedder, error) {
	switch cfg.Provider {
	case "ollama", "":
		slog.Info("using Ollama embedder",
			"base_url", cfg.Ollama.BaseURL,
			"model", cfg.Ollama.EmbedModel)
		return rag.NewOllamaEmbedder(cfg.Ollama.BaseURL, cfg.Ollama.EmbedModel), nil

	case "openai":
		if cfg.OpenAI.APIKey == "" {
			return nil, fmt.Errorf("ai.provider=openai requires ai.openai.api_key to be set")
		}
		slog.Info("using OpenAI embedder",
			"model", cfg.OpenAI.EmbedModel,
			"base_url", cfg.OpenAI.BaseURL)
		return rag.NewOpenAIEmbedder(cfg.OpenAI.APIKey, cfg.OpenAI.EmbedModel, cfg.OpenAI.BaseURL), nil

	default:
		return nil, fmt.Errorf("unknown ai.provider %q (valid: ollama, openai)", cfg.Provider)
	}
}
