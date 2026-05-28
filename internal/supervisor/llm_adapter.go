package supervisor

// llm_adapter.go provides a concrete LLMClient implementation that calls LLM
// providers directly over HTTP. The system prompt for AnalyzeEvent includes the
// full pkg/sdk source so the LLM understands the compilation target when it
// emits builder-mode code proposals.
//
// Configuration mirrors the llm integration plugin schema:
//
//	integrations:
//	  llm:
//	    evaluator_category: "reasoning"   # which category to use (default: "fast")
//	    categories:
//	      fast:
//	        - provider: openai
//	          model: gpt-4o-mini
//	      reasoning:
//	        - provider: openai
//	          model: o3-mini
//	    providers:
//	      openai:
//	        base_url: "https://api.openai.com/v1"
//	        key: "<api-key>"
//	      ollama:
//	        base_url: "http://localhost:11434"
//	      gemini:
//	        api_key: "<api-key>"

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"
)

// pkgSDKSource is embedded verbatim in the evaluator system prompt so the LLM
// knows the exact interface it must compile against when generating actuator code.
const pkgSDKSource = `
// ---- pkg/sdk/actuator.go ----
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
	ID           string       ` + "`json:\"id\"`" + `
	Description  string       ` + "`json:\"description\"`" + `
	Capabilities []Capability ` + "`json:\"capabilities\"`" + `
}

type Capability struct {
	Resource    string ` + "`json:\"resource\"`" + `
	Description string ` + "`json:\"description\"`" + `
}

type Event struct {
	ID     string         ` + "`json:\"id\"`" + `
	Source string         ` + "`json:\"source\"`" + `
	Type   string         ` + "`json:\"type\"`" + `
	Time   time.Time      ` + "`json:\"time\"`" + `
	Data   map[string]any ` + "`json:\"data\"`" + `
}

type Result struct {
	Success bool          ` + "`json:\"success\"`" + `
	Output  string        ` + "`json:\"output,omitempty\"`" + `
	Retry   bool          ` + "`json:\"retry,omitempty\"`" + `
	Delay   time.Duration ` + "`json:\"delay,omitempty\"`" + `
	Data    map[string]any ` + "`json:\"data,omitempty\"`" + `
}

// ---- pkg/sdk/serve.go ----
package sdk

import "github.com/hashicorp/go-plugin"

// Serve starts the actuator binary's plugin server. Call this from main().
func Serve(impl Actuator) {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"actuator": &ActuatorPlugin{Impl: impl},
		},
	})
}
`

// evaluatorSystemPrompt is the system prompt used for all AnalyzeEvent calls.
const evaluatorSystemPrompt = `You are the Evaluator for Clara, an autonomous self-modifying assistant daemon.

Your job: given a CloudEvent and a list of known actuator IDs, decide what to do.

## Response format

Respond with a single JSON object (no markdown fences):

{
  "action": "invoke" | "build" | "ignore",
  "actuator_id": "<string>",
  "heuristic_ttl_seconds": <number>,
  "proposed_code": {
    "main.go": "<full Go source>"
  }
}

### action values
- "invoke"  — an existing actuator handles this event; set actuator_id to its ID.
- "build"   — no actuator exists; generate a new one; set actuator_id to a kebab-case
              identifier and populate proposed_code["main.go"] with complete Go source.
- "ignore"  — the event is noise; no action needed.

### proposed_code rules (build action only)
The generated main.go MUST:
1. Be a standalone Go binary (package main).
2. Import and call sdk.Serve() with an implementation of sdk.Actuator.
3. Declare all required capabilities in Manifest().
4. Compile with: go build -o <actuator_id> .
5. Have no external dependencies beyond the clara SDK and the Go standard library.

## Clara SDK (compile target)
` + pkgSDKSource

// thinkTagPattern strips <think>...</think> blocks emitted by some reasoning models.
var thinkTagPattern = regexp.MustCompile(`(?s)<think>.*?</think>\s*`)

// --------------------------------------------------------------------------
// Config types (mirrors the llm integration plugin schema)
// --------------------------------------------------------------------------

type llmAdapterModelConfig struct {
	Provider string              `json:"provider"`
	Model    string              `json:"model"`
	Thinking *llmAdapterThinking `json:"thinking,omitempty"`
}

// llmAdapterThinking mirrors ThinkingConfig in the llm integration plugin.
// See cmd/integrations/llm/llm.go for the canonical documentation.
type llmAdapterThinking struct {
	Level   string `json:"level,omitempty"`
	Budget  *int   `json:"budget,omitempty"`
	Enabled *bool  `json:"enabled,omitempty"`
}

type llmAdapterProviderConfig struct {
	APIKey  string `json:"api_key,omitempty"`
	BaseURL string `json:"base_url,omitempty"`
	Key     string `json:"key,omitempty"`
}

type llmAdapterConfig struct {
	EvaluatorCategory string                              `json:"evaluator_category"`
	Categories        map[string][]llmAdapterModelConfig  `json:"categories"`
	Providers         map[string]llmAdapterProviderConfig `json:"providers"`
}

// --------------------------------------------------------------------------
// LLMAdapter — concrete LLMClient
// --------------------------------------------------------------------------

// LLMAdapter implements LLMClient by calling LLM providers over HTTP.
type LLMAdapter struct {
	log    zerolog.Logger
	config llmAdapterConfig
}

// NewLLMAdapter creates an LLMAdapter from the raw integrations["llm"] config map.
// Returns (nil, nil) when no llm config is provided; the caller should fall back
// to NoopLLMClient() in that case.
func NewLLMAdapter(log zerolog.Logger, rawCfg map[string]any) (*LLMAdapter, error) {
	if len(rawCfg) == 0 {
		return nil, nil
	}

	b, err := json.Marshal(rawCfg)
	if err != nil {
		return nil, errors.Wrap(err, "marshal llm config")
	}

	var cfg llmAdapterConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, errors.Wrap(err, "unmarshal llm config")
	}

	if cfg.EvaluatorCategory == "" {
		cfg.EvaluatorCategory = "fast"
	}

	return &LLMAdapter{
		log:    log.With().Str("component", "llm_adapter").Logger(),
		config: cfg,
	}, nil
}

// AnalyzeEvent implements LLMClient. It serialises the event and history into a
// user message, calls the configured LLM, and parses the JSON response into an
// AnalysisResult.
func (a *LLMAdapter) AnalyzeEvent(
	ctx context.Context,
	ev CloudEvent,
	history []string,
) (AnalysisResult, error) {
	userMsg, err := a.buildUserMessage(ev, history)
	if err != nil {
		return AnalysisResult{}, errors.Wrap(err, "build user message")
	}

	raw, err := a.generate(ctx, []llmMessage{
		{Role: "system", Content: evaluatorSystemPrompt},
		{Role: "user", Content: userMsg},
	})
	if err != nil {
		return AnalysisResult{}, errors.Wrap(err, "llm generate")
	}

	// Strip think tags before parsing
	raw = strings.TrimSpace(thinkTagPattern.ReplaceAllString(raw, ""))

	return parseAnalysisResult(raw)
}

// --------------------------------------------------------------------------
// Internal helpers
// --------------------------------------------------------------------------

type llmMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (a *LLMAdapter) buildUserMessage(ev CloudEvent, history []string) (string, error) {
	evJSON, err := json.MarshalIndent(ev, "", "  ")
	if err != nil {
		return "", errors.Wrap(err, "marshal cloud event")
	}

	var sb strings.Builder
	sb.WriteString("## Incoming CloudEvent\n\n```json\n")
	sb.Write(evJSON)
	sb.WriteString("\n```\n\n")

	if len(history) > 0 {
		sb.WriteString("## History\n\n")
		for _, h := range history {
			sb.WriteString("- ")
			sb.WriteString(h)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Respond with the JSON decision object only.")
	return sb.String(), nil
}

// generate picks the first working model in the evaluator category and returns
// the raw text completion.
func (a *LLMAdapter) generate(ctx context.Context, messages []llmMessage) (string, error) {
	category := a.config.EvaluatorCategory
	models, ok := a.config.Categories[category]
	if !ok || len(models) == 0 {
		return "", fmt.Errorf("no models configured for evaluator category %q", category)
	}

	var lastErr error
	for _, m := range models {
		pCfg, ok := a.config.Providers[m.Provider]
		if !ok {
			lastErr = fmt.Errorf("provider %q not configured", m.Provider)
			continue
		}

		text, err := a.callProvider(ctx, m.Provider, pCfg, m, messages)
		if err == nil {
			return text, nil
		}
		a.log.Warn().Err(err).Str("provider", m.Provider).Str("model", m.Model).
			Msg("llm provider call failed; trying next")
		lastErr = err
	}

	return "", errors.Wrapf(lastErr, "all models in category %q failed", category)
}

func (a *LLMAdapter) callProvider(
	ctx context.Context,
	providerName string,
	cfg llmAdapterProviderConfig,
	m llmAdapterModelConfig,
	messages []llmMessage,
) (string, error) {
	switch providerName {
	case "openai":
		return a.callOpenAI(ctx, cfg, m.Model, messages)
	case "ollama":
		return a.callOllama(ctx, cfg, m, messages)
	case "gemini":
		return a.callGemini(ctx, cfg, m, messages)
	default:
		return "", fmt.Errorf("unsupported provider: %s", providerName)
	}
}

// --- OpenAI-compatible ---

func (a *LLMAdapter) callOpenAI(
	ctx context.Context,
	cfg llmAdapterProviderConfig,
	model string,
	messages []llmMessage,
) (string, error) {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	url := strings.TrimSuffix(baseURL, "/") + "/chat/completions"

	body := map[string]any{
		"model":    model,
		"messages": messages,
	}
	b, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return "", errors.Wrap(err, "create openai request")
	}
	req.Header.Set("Content-Type", "application/json")
	key := cfg.Key
	if key == "" {
		key = cfg.APIKey
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", errors.Wrap(err, "post to openai")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("openai status %d: %s", resp.StatusCode, string(body))
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", errors.Wrap(err, "decode openai response")
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("openai returned no choices")
	}
	return out.Choices[0].Message.Content, nil
}

// --- Ollama ---

func (a *LLMAdapter) callOllama(
	ctx context.Context,
	cfg llmAdapterProviderConfig,
	m llmAdapterModelConfig,
	messages []llmMessage,
) (string, error) {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	url := strings.TrimSuffix(baseURL, "/") + "/api/chat"

	body := map[string]any{
		"model":    m.Model,
		"messages": messages,
		"stream":   false,
	}
	if m.Thinking != nil && m.Thinking.Enabled != nil {
		body["think"] = *m.Thinking.Enabled
	}
	b, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return "", errors.Wrap(err, "create ollama request")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", errors.Wrap(err, "post to ollama")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama status %d: %s", resp.StatusCode, string(body))
	}

	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", errors.Wrap(err, "decode ollama response")
	}
	content := strings.TrimSpace(thinkTagPattern.ReplaceAllString(out.Message.Content, ""))
	return content, nil
}

// --- Gemini ---

func (a *LLMAdapter) callGemini(
	ctx context.Context,
	cfg llmAdapterProviderConfig,
	m llmAdapterModelConfig,
	messages []llmMessage,
) (string, error) {
	if cfg.APIKey == "" {
		return "", fmt.Errorf("gemini api_key not configured")
	}

	url := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		m.Model,
		cfg.APIKey,
	)

	type Part struct {
		Text string `json:"text"`
	}
	type Content struct {
		Role  string `json:"role"`
		Parts []Part `json:"parts"`
	}
	type ThinkingConfigWire struct {
		ThinkingLevel  string `json:"thinkingLevel,omitempty"`
		ThinkingBudget *int   `json:"thinkingBudget,omitempty"`
	}
	type GenerationConfig struct {
		ThinkingConfig *ThinkingConfigWire `json:"thinkingConfig,omitempty"`
	}
	type GeminiBody struct {
		Contents         []Content         `json:"contents"`
		GenerationConfig *GenerationConfig `json:"generationConfig,omitempty"`
	}

	var contents []Content
	for _, msg := range messages {
		role := msg.Role
		if role == "assistant" {
			role = "model"
		}
		if role == "system" {
			role = "user"
		}
		contents = append(contents, Content{Role: role, Parts: []Part{{Text: msg.Content}}})
	}

	payload := GeminiBody{Contents: contents}
	if m.Thinking != nil {
		tc := &ThinkingConfigWire{}
		if m.Thinking.Level != "" {
			tc.ThinkingLevel = m.Thinking.Level
		}
		if m.Thinking.Budget != nil {
			tc.ThinkingBudget = m.Thinking.Budget
		}
		payload.GenerationConfig = &GenerationConfig{ThinkingConfig: tc}
	}

	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return "", errors.Wrap(err, "create gemini request")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", errors.Wrap(err, "post to gemini")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("gemini status %d: %s", resp.StatusCode, string(body))
	}

	var out struct {
		Candidates []struct {
			Content struct {
				Parts []Part `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", errors.Wrap(err, "decode gemini response")
	}
	if len(out.Candidates) == 0 || len(out.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("gemini returned no candidates")
	}
	return out.Candidates[0].Content.Parts[0].Text, nil
}

// --------------------------------------------------------------------------
// Response parsing
// --------------------------------------------------------------------------

// rawAnalysisResult is the wire format returned by the LLM.
type rawAnalysisResult struct {
	Action               string            `json:"action"`
	ActuatorID           string            `json:"actuator_id"`
	HeuristicTTLSeconds  float64           `json:"heuristic_ttl_seconds"`
	ProposedCode         map[string]string `json:"proposed_code"`
}

func parseAnalysisResult(raw string) (AnalysisResult, error) {
	// Strip markdown code fences if the model wrapped output anyway.
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		lines := strings.SplitN(raw, "\n", 2)
		if len(lines) == 2 {
			raw = lines[1]
		}
		raw = strings.TrimSuffix(raw, "```")
		raw = strings.TrimSpace(raw)
	}

	var r rawAnalysisResult
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return AnalysisResult{}, errors.Wrapf(err, "parse LLM JSON response: %q", raw)
	}

	if r.Action == "" {
		r.Action = "ignore"
	}

	ttl := time.Duration(r.HeuristicTTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = 1 * time.Hour
	}

	return AnalysisResult{
		Action:       r.Action,
		ActuatorID:   r.ActuatorID,
		HeuristicTTL: ttl,
		ProposedCode: r.ProposedCode,
	}, nil
}
