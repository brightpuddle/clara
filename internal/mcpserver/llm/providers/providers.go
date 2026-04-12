package providers

import (
	"context"
	"encoding/json"
	"strings"
)

const ProviderGemini = "gemini"

type GenerateRequest struct {
	Prompt          string
	System          string
	Category        string
	Provider        string
	Model           string
	Decode          string
	Temperature     *float64
	MaxOutputTokens *int
	Images          []string
}

type GenerateResult struct {
	Text         string `json:"text"`
	ProviderUsed string `json:"provider_used"`
	ModelUsed    string `json:"model_used"`
	FinishReason string `json:"finish_reason,omitempty"`
	Parsed       any    `json:"parsed,omitempty"`
}

type EmbedRequest struct {
	Inputs   []string
	Category string
	Provider string
	Model    string
}

type EmbedResult struct {
	Embeddings   [][]float64 `json:"embeddings"`
	ProviderUsed string      `json:"provider_used"`
	ModelUsed    string      `json:"model_used"`
}

type RateLimitError struct {
	Message string
}

func (e *RateLimitError) Error() string {
	return e.Message
}

type Capabilities struct {
	Generate bool `json:"generate"`
	Vision   bool `json:"vision"`
	Embed    bool `json:"embed"`
}

type Status struct {
	Provider     string       `json:"provider"`
	Status       string       `json:"status"`
	Message      string       `json:"message,omitempty"`
	Models       []string     `json:"models,omitempty"`
	Capabilities Capabilities `json:"capabilities"`
}

type Provider interface {
	Name() string
	Capabilities() Capabilities
	Status(ctx context.Context) Status
	Generate(ctx context.Context, req GenerateRequest) (GenerateResult, error)
	GenerateVision(ctx context.Context, req GenerateRequest) (GenerateResult, error)
	Embed(ctx context.Context, req EmbedRequest) (EmbedResult, error)
}

func DecodeJSON(text string) any {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}

	// Strip markdown code blocks if present (e.g. ```json\n{...}\n```)
	if strings.Contains(trimmed, "```") {
		start := strings.Index(trimmed, "```")
		end := strings.LastIndex(trimmed, "```")
		if start != -1 && end > start {
			// Extract content between backticks
			content := trimmed[start+3 : end]

			// Find the first '{' or '[' in the content to skip language tags or text
			firstBrace := strings.IndexAny(content, "{[")
			if firstBrace != -1 {
				trimmed = content[firstBrace:]
			} else {
				trimmed = content
			}
		}
	}

	// Remove individual backticks and whitespace
	trimmed = strings.Trim(trimmed, "`")
	trimmed = strings.TrimSpace(trimmed)

	// Support both objects and arrays by finding the outer boundaries
	first := strings.IndexAny(trimmed, "{[")
	last := strings.LastIndexAny(trimmed, "}]")
	if first != -1 && last != -1 && last >= first {
		trimmed = trimmed[first : last+1]
	}

	if trimmed == "" || !((strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
		(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]"))) {
		return nil
	}

	var value any
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return nil
	}
	return value
}
