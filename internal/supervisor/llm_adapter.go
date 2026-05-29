package supervisor

// llm_adapter.go — concrete LLMClient implementation for the Evaluator.
//
// It drives the same provider backends as the llm integration plugin (Gemini,
// Ollama, OpenAI-compatible) but lives in-process so the Evaluator has no
// dependency on the plugin subprocess. Configuration is the same YAML/JSON
// block that the llm integration uses:
//
//	integrations:
//	  llm:
//	    categories:
//	      evaluator:
//	        - provider: gemini
//	          model: gemini-2.0-flash
//	    providers:
//	      gemini:
//	        api_key: "..."
//
// AnalyzeEvent builds a structured prompt that includes the pkg/sdk source
// verbatim (embedded at compile time) so the LLM understands the compilation
// target when it proposes new Actuator code.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"
)

// sdkSource is the pkg/sdk/actuator.go source embedded verbatim into the
// Evaluator system prompt so the LLM knows the exact contract it must target
// when generating new Actuator code.
const sdkSource = `// pkg/sdk/actuator.go — Actuator contract for Clara V2.
package sdk

import (
	"context"
	"time"
)

type Actuator interface {
	Manifest() ActuatorManifest
	Execute(ctx context.Context, event Event) (Result, error)
}

type ActuatorManifest struct {
	ID           string       ` + "`" + `json:"id"` + "`" + `
	Description  string       ` + "`" + `json:"description"` + "`" + `
	Capabilities []Capability ` + "`" + `json:"capabilities"` + "`" + `
}

type Capability struct {
	Resource    string ` + "`" + `json:"resource"` + "`" + `
	Description string ` + "`" + `json:"description"` + "`" + `
}

type Event struct {
	ID     string         ` + "`" + `json:"id"` + "`" + `
	Source string         ` + "`" + `json:"source"` + "`" + `
	Type   string         ` + "`" + `json:"type"` + "`" + `
	Time   time.Time      ` + "`" + `json:"time"` + "`" + `
	Data   map[string]any ` + "`" + `json:"data"` + "`" + `
}

type Result struct {
	Success bool           ` + "`" + `json:"success"` + "`" + `
	Output  string         ` + "`" + `json:"output,omitempty"` + "`" + `
	Retry   bool           ` + "`" + `json:"retry,omitempty"` + "`" + `
	Delay   time.Duration  ` + "`" + `json:"delay,omitempty"` + "`" + `
	Data    map[string]any ` + "`" + `json:"data,omitempty"` + "`" + `
}

// Serve wires impl into hashicorp/go-plugin and blocks until the daemon kills it.
func Serve(impl Actuator) { ... }
`

// evaluatorSystemPrompt is the fixed system prompt for AnalyzeEvent calls.
const evaluatorSystemPrompt = `You are the Clara Evaluator — the routing brain of an autonomous assistant daemon.

Your job is to decide what to do with an incoming CloudEvent. You must respond
with a single JSON object (no markdown, no prose) matching this schema:

{
  "action":       "invoke" | "build" | "ignore",
  "actuator_id":  "<stable kebab-case id>",
  "proposed_code": {
    "<filename>.go": "<full Go source>"
  },
  "heuristic_ttl": <nanoseconds as integer>
}

Rules:
- "invoke"  — an existing actuator binary can handle this event. Set actuator_id.
- "build"   — no existing actuator fits; generate a new one. Set actuator_id and
              proposed_code (map of filename → full Go source). The code MUST
              import "github.com/brightpuddle/clara/pkg/sdk" and implement the sdk.Actuator interface shown below.
- "ignore"  — the event is noise; no action needed.
- When generating Go source code for the "build" action, the import path for the Clara Actuator SDK MUST be exactly "github.com/brightpuddle/clara/pkg/sdk". Do NOT use "github.com/clara/sdk", "github.com/clara-v2/pkg/sdk", or any other path.
- heuristic_ttl is optional. Omit or set 0 to use the default (1 hour).

Actuator SDK contract (compile target):

` + "```go\n" + sdkSource + "```" + `

Every generated actuator must call sdk.Serve(&MyActuator{}) from main().
`

// thinkRe strips <think>…</think> reasoning blocks emitted by some models.
var thinkRe = regexp.MustCompile(`(?s)<think>.*?</think>\s*`)

// ─── config types (mirrors llm integration plugin) ───────────────────────────

type llmAdapterModelConfig struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type llmAdapterProviderConfig struct {
	APIKey  string `json:"api_key,omitempty"`
	BaseURL string `json:"base_url,omitempty"`
	Key     string `json:"key,omitempty"`
}

type llmAdapterConfig struct {
	Categories map[string][]llmAdapterModelConfig    `json:"categories"`
	Providers  map[string]llmAdapterProviderConfig   `json:"providers"`
}

// ─── LLMAdapter ──────────────────────────────────────────────────────────────

// LLMAdapter is a concrete LLMClient that calls LLM APIs directly (no plugin
// subprocess) using the same provider logic as the llm integration plugin.
type LLMAdapter struct {
	log    zerolog.Logger
	config llmAdapterConfig
}

// NewLLMAdapter creates an LLMAdapter from the raw map[string]any config block
// for the "llm" integration (as read from ~/.config/clara/config.yaml).
//
// Returns an error only when rawCfg is non-nil but malformed; nil rawCfg is
// silently treated as an empty config (will fail at request time with a clear
// error).
func NewLLMAdapter(log zerolog.Logger, rawCfg map[string]any) (*LLMAdapter, error) {
	a := &LLMAdapter{
		log: log.With().Str("component", "llm_adapter").Logger(),
	}
	if rawCfg == nil {
		return a, nil
	}
	b, err := json.Marshal(rawCfg)
	if err != nil {
		return nil, errors.Wrap(err, "marshal llm config")
	}
	if err := json.Unmarshal(b, &a.config); err != nil {
		return nil, errors.Wrap(err, "unmarshal llm config")
	}
	return a, nil
}

// AnalyzeEvent implements LLMClient. It calls the "evaluator" category (falling
// back to "reasoning", then "fast") and parses the JSON response into an
// AnalysisResult.
func (a *LLMAdapter) AnalyzeEvent(
	ctx context.Context,
	ev CloudEvent,
	history []string,
) (AnalysisResult, error) {
	evJSON, _ := json.MarshalIndent(ev, "", "  ")

	userMsg := fmt.Sprintf(
		"Event:\n```json\n%s\n```\n\nHistory:\n%s",
		string(evJSON),
		strings.Join(history, "\n"),
	)

	text, err := a.generate(ctx, userMsg)
	if err != nil {
		return AnalysisResult{}, err
	}

	text = strings.TrimSpace(thinkRe.ReplaceAllString(text, ""))

	// Strip markdown code fences if present.
	if strings.HasPrefix(text, "```") {
		lines := strings.SplitN(text, "\n", 2)
		if len(lines) == 2 {
			text = lines[1]
		}
		text = strings.TrimSuffix(strings.TrimSpace(text), "```")
	}

	var result AnalysisResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return AnalysisResult{}, errors.Wrapf(err, "parse LLM response as AnalysisResult: %q", text)
	}
	return result, nil
}

// generate sends a chat completion request to the first working model in the
// "evaluator" → "reasoning" → "fast" category preference list.
func (a *LLMAdapter) generate(ctx context.Context, userMsg string) (string, error) {
	categories := []string{"evaluator", "reasoning", "fast"}
	var lastErr error
	for _, cat := range categories {
		models, ok := a.config.Categories[cat]
		if !ok || len(models) == 0 {
			continue
		}
		for _, m := range models {
			provider, ok := a.config.Providers[m.Provider]
			if !ok {
				lastErr = errors.Newf("provider %q not configured", m.Provider)
				continue
			}
			text, err := a.callProvider(ctx, m.Provider, provider, m.Model, userMsg)
			if err != nil {
				a.log.Warn().Err(err).Str("provider", m.Provider).Str("model", m.Model).
					Msg("LLM call failed; trying next model")
				lastErr = err
				continue
			}
			return text, nil
		}
	}
	if lastErr != nil {
		return "", errors.Wrap(lastErr, "all LLM models failed")
	}
	return "", errors.New("no LLM models configured in categories [evaluator, reasoning, fast]")
}

func (a *LLMAdapter) callProvider(
	ctx context.Context,
	providerName string,
	cfg llmAdapterProviderConfig,
	model string,
	userMsg string,
) (string, error) {
	switch providerName {
	case "gemini":
		return a.callGemini(ctx, cfg, model, userMsg)
	case "ollama":
		return a.callOllama(ctx, cfg, model, userMsg)
	case "openai":
		return a.callOpenAI(ctx, cfg, model, userMsg)
	default:
		return "", errors.Newf("unsupported provider: %q", providerName)
	}
}

// ─── Gemini ──────────────────────────────────────────────────────────────────

func (a *LLMAdapter) callGemini(
	ctx context.Context,
	cfg llmAdapterProviderConfig,
	model string,
	userMsg string,
) (string, error) {
	if cfg.APIKey == "" {
		return "", errors.New("gemini api_key not configured")
	}
	url := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		model, cfg.APIKey,
	)

	type Part struct {
		Text string `json:"text"`
	}
	type Content struct {
		Role  string `json:"role"`
		Parts []Part `json:"parts"`
	}
	reqBody := map[string]any{
		"system_instruction": map[string]any{
			"parts": []Part{{Text: evaluatorSystemPrompt}},
		},
		"contents": []Content{
			{Role: "user", Parts: []Part{{Text: userMsg}}},
		},
	}
	return a.doPost(ctx, url, nil, reqBody, func(b []byte) (string, error) {
		var resp struct {
			Candidates []struct {
				Content struct {
					Parts []Part `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		if err := json.Unmarshal(b, &resp); err != nil {
			return "", errors.Wrap(err, "decode gemini response")
		}
		if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
			return "", errors.New("gemini returned no candidates")
		}
		return resp.Candidates[0].Content.Parts[0].Text, nil
	})
}

// ─── Ollama ──────────────────────────────────────────────────────────────────

func (a *LLMAdapter) callOllama(
	ctx context.Context,
	cfg llmAdapterProviderConfig,
	model string,
	userMsg string,
) (string, error) {
	base := cfg.BaseURL
	if base == "" {
		base = "http://localhost:11434"
	}
	url := strings.TrimSuffix(base, "/") + "/api/chat"
	reqBody := map[string]any{
		"model":  model,
		"stream": false,
		"messages": []map[string]string{
			{"role": "system", "content": evaluatorSystemPrompt},
			{"role": "user", "content": userMsg},
		},
	}
	return a.doPost(ctx, url, nil, reqBody, func(b []byte) (string, error) {
		var resp struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(b, &resp); err != nil {
			return "", errors.Wrap(err, "decode ollama response")
		}
		return strings.TrimSpace(thinkRe.ReplaceAllString(resp.Message.Content, "")), nil
	})
}

// ─── OpenAI-compatible ───────────────────────────────────────────────────────

func (a *LLMAdapter) callOpenAI(
	ctx context.Context,
	cfg llmAdapterProviderConfig,
	model string,
	userMsg string,
) (string, error) {
	base := cfg.BaseURL
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	url := strings.TrimSuffix(base, "/") + "/chat/completions"
	reqBody := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": evaluatorSystemPrompt},
			{"role": "user", "content": userMsg},
		},
	}
	key := cfg.Key
	if key == "" {
		key = cfg.APIKey
	}
	headers := map[string]string{}
	if key != "" {
		headers["Authorization"] = "Bearer " + key
	}
	return a.doPost(ctx, url, headers, reqBody, func(b []byte) (string, error) {
		var resp struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(b, &resp); err != nil {
			return "", errors.Wrap(err, "decode openai response")
		}
		if len(resp.Choices) == 0 {
			return "", errors.New("openai returned no choices")
		}
		return resp.Choices[0].Message.Content, nil
	})
}

// ─── shared HTTP helper ───────────────────────────────────────────────────────

func (a *LLMAdapter) doPost(
	ctx context.Context,
	url string,
	headers map[string]string,
	body any,
	parse func([]byte) (string, error),
) (string, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return "", errors.Wrap(err, "marshal request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return "", errors.Wrap(err, "create request")
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", errors.Wrap(err, "HTTP POST")
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", errors.Wrap(err, "read response body")
	}
	if resp.StatusCode != http.StatusOK {
		return "", errors.Newf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return parse(respBody)
}
