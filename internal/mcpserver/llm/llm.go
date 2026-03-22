package llm

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/brightpuddle/clara/internal/mcpserver/llm/providers"
)

const (
	Description          = "Built-in LLM MCP server with Gemini-backed text and vision generation."
	DefaultProvider      = "gemini"
	DefaultGeminiModel   = providers.DefaultGeminiModel
	DefaultGeminiBaseURL = providers.DefaultGeminiBaseURL
)

type Options struct {
	DefaultProvider string
	GeminiAPIKey    string
	GeminiModel     string
	GeminiBaseURL   string
}

type Service struct {
	providers       map[string]providers.Provider
	defaultProvider string
	routes          map[string]string
}

func New(opts Options) *Service {
	defaultProvider := strings.TrimSpace(opts.DefaultProvider)
	if defaultProvider == "" {
		defaultProvider = DefaultProvider
	}

	providerSet := map[string]providers.Provider{
		providers.ProviderGemini: providers.NewGemini(
			providers.GeminiOptions{
				APIKey:  opts.GeminiAPIKey,
				Model:   opts.GeminiModel,
				BaseURL: opts.GeminiBaseURL,
			},
		),
	}

	return &Service{
		providers:       providerSet,
		defaultProvider: defaultProvider,
		routes: map[string]string{
			"vision":        providers.ProviderGemini,
			"general-small": providers.ProviderGemini,
			"general-large": providers.ProviderGemini,
			"coding":        providers.ProviderGemini,
		},
	}
}

func (s *Service) NewServer() *server.MCPServer {
	mcpServer := server.NewMCPServer("clara-llm", "0.1.0", server.WithToolCapabilities(true))

	mcpServer.AddTool(mcp.NewTool(
		"generate",
		mcp.WithDescription("Generate text from a prompt using the configured LLM provider."),
		mcp.WithString("prompt", mcp.Required(), mcp.Description("Prompt text to send to the model.")),
		mcp.WithString("system", mcp.Description("Optional system instruction for the model.")),
		mcp.WithString(
			"category",
			mcp.Description("Optional routing category such as general-small, general-large, coding, or vision."),
		),
		mcp.WithString("provider", mcp.Description("Optional provider override, for example gemini.")),
		mcp.WithString("model", mcp.Description("Optional model override.")),
		mcp.WithNumber("temperature", mcp.Description("Optional temperature override.")),
		mcp.WithNumber(
			"max_output_tokens",
			mcp.Description("Optional maximum number of output tokens."),
		),
	), s.handleGenerate)

	mcpServer.AddTool(mcp.NewTool(
		"generate_vision",
		mcp.WithDescription("Generate text from a prompt plus one or more local image files."),
		mcp.WithString("prompt", mcp.Required(), mcp.Description("Prompt text to send to the model.")),
		mcp.WithArray(
			"images",
			mcp.Required(),
			mcp.Description("Array of local image file paths to include in the request."),
		),
		mcp.WithString("system", mcp.Description("Optional system instruction for the model.")),
		mcp.WithString(
			"category",
			mcp.Description("Optional routing category such as general-small, general-large, coding, or vision."),
		),
		mcp.WithString("provider", mcp.Description("Optional provider override, for example gemini.")),
		mcp.WithString("model", mcp.Description("Optional model override.")),
		mcp.WithNumber("temperature", mcp.Description("Optional temperature override.")),
		mcp.WithNumber(
			"max_output_tokens",
			mcp.Description("Optional maximum number of output tokens."),
		),
	), s.handleGenerateVision)

	mcpServer.AddTool(mcp.NewTool(
		"providers",
		mcp.WithDescription("List configured LLM providers and their availability."),
	), s.handleProviders)

	return mcpServer
}

func (s *Service) handleGenerate(
	ctx context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	call, err := parseGenerateRequest(req.GetArguments(), false)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	provider, err := s.resolveProvider(call)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result, err := provider.Generate(ctx, call)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultStructuredOnly(result), nil
}

func (s *Service) handleGenerateVision(
	ctx context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	call, err := parseGenerateRequest(req.GetArguments(), true)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	provider, err := s.resolveProvider(call)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if !provider.Capabilities().Vision {
		return mcp.NewToolResultError(
			fmt.Sprintf("provider %q does not support vision generation", provider.Name()),
		), nil
	}

	result, err := provider.GenerateVision(ctx, call)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultStructuredOnly(result), nil
}

func (s *Service) handleProviders(
	ctx context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	result := make([]map[string]any, 0, len(s.providers))
	for _, name := range providerNames(s.providers) {
		provider := s.providers[name]
		status := provider.Status(ctx)
		result = append(result, map[string]any{
			"provider":     status.Provider,
			"status":       status.Status,
			"models":       status.Models,
			"capabilities": status.Capabilities,
			"message":      status.Message,
		})
	}
	return mcp.NewToolResultStructuredOnly(result), nil
}

func (s *Service) resolveProvider(call providers.GenerateRequest) (providers.Provider, error) {
	providerName := strings.TrimSpace(call.Provider)
	if providerName == "" {
		if routed := strings.TrimSpace(s.routes[strings.TrimSpace(call.Category)]); routed != "" {
			providerName = routed
		} else {
			providerName = s.defaultProvider
		}
	}

	provider, ok := s.providers[providerName]
	if !ok {
		return nil, errors.Newf("unknown llm provider %q", providerName)
	}
	return provider, nil
}

func parseGenerateRequest(args map[string]any, requireImages bool) (providers.GenerateRequest, error) {
	prompt, ok := args["prompt"].(string)
	if !ok || strings.TrimSpace(prompt) == "" {
		return providers.GenerateRequest{}, errors.New("prompt must be a non-empty string")
	}

	call := providers.GenerateRequest{
		Prompt:   prompt,
		System:   stringArg(args, "system"),
		Category: stringArg(args, "category"),
		Provider: stringArg(args, "provider"),
		Model:    stringArg(args, "model"),
	}

	if temperature, ok := numberArg(args, "temperature"); ok {
		call.Temperature = &temperature
	}
	if maxTokens, ok := intArg(args, "max_output_tokens"); ok {
		call.MaxOutputTokens = &maxTokens
	}

	images, err := stringSliceArg(args, "images")
	if err != nil {
		return providers.GenerateRequest{}, err
	}
	call.Images = images

	if requireImages && len(call.Images) == 0 {
		return providers.GenerateRequest{}, errors.New("images must be a non-empty array of file paths")
	}

	return call, nil
}

func stringArg(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return strings.TrimSpace(value)
}

func numberArg(args map[string]any, key string) (float64, bool) {
	value, ok := args[key].(float64)
	return value, ok
}

func intArg(args map[string]any, key string) (int, bool) {
	value, ok := args[key].(float64)
	if !ok {
		return 0, false
	}
	return int(value), true
}

func stringSliceArg(args map[string]any, key string) ([]string, error) {
	raw, ok := args[key]
	if !ok {
		return nil, nil
	}

	items, ok := raw.([]any)
	if !ok {
		return nil, errors.Newf("%s must be an array of strings", key)
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		value, ok := item.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return nil, errors.Newf("%s must contain only non-empty strings", key)
		}
		result = append(result, value)
	}
	return result, nil
}

func providerNames(providerSet map[string]providers.Provider) []string {
	names := make([]string, 0, len(providerSet))
	for name := range providerSet {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
