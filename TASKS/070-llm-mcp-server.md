---
plan_recommended: true
---

# Unified LLM MCP Server

## Planning Context

This is a significant architectural component. The existing `internal/mcpserver/ollama/` server is
narrowly scoped to Ollama. The goal is to replace it with a unified `llm` MCP server
(`internal/mcpserver/llm/`) that abstracts multiple providers behind a consistent tool interface.
Planning should address:

- **Provider interface design**
- **Category-to-model routing**
- **Gemini free tier tracking**
- **Copilot API access**
- **Ollama resource protection**
- **Network availability**

## Context

There are many AI tasks across Clara: new ad research (Gemini vision), renewal evaluation (Ollama
batch), Messenger classification (Ollama), email triage, GitHub issue summaries, ZK backlinks, etc.
Each has different requirements: vision vs. text, speed vs. quality, free-tier vs. paid, on-machine
vs. network. A unified LLM MCP server removes the per-intent concern of which model/provider to
use and adds resource protection centrally.

This server does **not** replace the prototype direct API calls in task 020; those will be
migrated to use this server after it exists.

## Tool Interface

### `llm.generate`
- Input: `prompt`, `system`, `category`, `provider`, `model`
- Output: `text`, `provider_used`, `model_used`, `tokens`

### `llm.generate_vision`
- Input: `prompt`, `system`, `images`, `category`, `provider`, `model`
- Output: same as `llm.generate`

### `llm.embed`
- Input: `text`, `category`
- Output: `embedding`, `provider_used`, `model_used`

### `llm.providers`
- Output: array of `{provider, status, models}`

## Configuration

```yaml
llm:
  categories:
    vision:
      - provider: gemini
        model: gemini-2.5-flash
    general-small:
      - provider: gemini
        model: gemini-2.5-flash
      - provider: ollama
        model: qwen3:8b
    general-large:
      - provider: ollama
        model: qwen3:32b
      - provider: gemini
        model: gemini-2.5-pro
    coding:
      - provider: copilot
        model: gpt-4o
    embeddings:
      - provider: ollama
        model: nomic-embed-text

  providers:
    gemini:
      api_key: "${GEMINI_API_KEY}"
      rate_limits:
        requests_per_minute: 15
        requests_per_day: 1500
    ollama:
      base_url: "http://localhost:11434"
      max_loaded_models: 1
    copilot:
      token: "${GITHUB_COPILOT_TOKEN}"
```

## Providers to Implement

1. **Ollama**
2. **Gemini**
3. **GitHub Copilot**

## Implementation Location

- `internal/mcpserver/llm/`
- `internal/mcpserver/llm/providers/`
- `cmd/clara/mcp.go` - add `clara mcp llm`
- Update `config.yaml.example`

## Acceptance Criteria

- `llm.generate` with `category: "general-small"` uses Gemini and falls back to Ollama if Gemini
  is rate-limited
- `llm.generate_vision` with image paths works against Gemini
- `llm.embed` returns a valid embedding vector from Ollama
- Rate limit tracking prevents Gemini calls after exhausting configured daily limit
- Concurrent requests to the same Ollama model are serialized, not stacked
- All providers are configurable via `config.yaml`
- `llm.providers` accurately reflects current availability of each provider
