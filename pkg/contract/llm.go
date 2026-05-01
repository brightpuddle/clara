package contract

import (
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

// Message represents a single message in a conversation.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// GenerateRequest represents a request for a completion.
type GenerateRequest struct {
	Messages    []Message `json:"messages"`
	Temperature float32   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

// GenerateResponse represents the response from a generation request.
type GenerateResponse struct {
	Message Message `json:"message"`
}

// VisionRequest represents a request for vision-based generation.
type VisionRequest struct {
	Messages    []Message `json:"messages"`
	ImageURL    string    `json:"image_url,omitempty"`    // URL or data:image/...
	ImageBase64 string    `json:"image_base64,omitempty"` // Raw base64 data
	Temperature float32   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

// LLMIntegrationPlugin is a thin plugin.Plugin wrapper for the llm integration.
type LLMIntegrationPlugin struct{ Impl Integration }

func (p *LLMIntegrationPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &IntegrationRPCServer{Impl: p.Impl}, nil
}

func (p *LLMIntegrationPlugin) Client(_ *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &IntegrationRPC{Client: c}, nil
}
