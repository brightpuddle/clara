package providers

import "context"

const ProviderGemini = "gemini"

type GenerateRequest struct {
	Prompt          string
	System          string
	Category        string
	Provider        string
	Model           string
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
}
