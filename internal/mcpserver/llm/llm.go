package llm

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/internal/config"
	"github.com/brightpuddle/clara/internal/mcpserver/llm/providers"
)

const (
	Description          = "Built-in LLM MCP server with Gemini-backed text and vision generation."
	DefaultProvider      = "gemini"
	DefaultGeminiModel   = providers.DefaultGeminiModel
	DefaultGeminiBaseURL = providers.DefaultGeminiBaseURL
)

type Options struct {
	Config *config.Config
	Logger zerolog.Logger
}

type routeRule struct {
	Provider string
	Model    string
}

type Service struct {
	providers       map[string]providers.Provider
	defaultProvider string
	routes          map[string][]routeRule
	log             zerolog.Logger
}

func New(opts Options) *Service {
	defaultProvider := DefaultProvider
	providerSet := make(map[string]providers.Provider)
	routes := make(map[string][]routeRule)

	if opts.Config != nil && opts.Config.LLM != nil {
		llmCfg := opts.Config.LLM

		if llmCfg.Providers.Gemini != nil {
			providerSet[providers.ProviderGemini] = providers.NewGemini(
				providers.GeminiOptions{
					APIKey:  llmCfg.Providers.Gemini.APIKey,
					Model:   llmCfg.Providers.Gemini.Model,
					BaseURL: llmCfg.Providers.Gemini.BaseURL,
					Logger:  opts.Logger,
				},
			)
		}

		if llmCfg.Providers.Ollama != nil {
			providerSet[providers.ProviderOllama] = providers.NewOllama(
				providers.OllamaOptions{
					BaseURL: llmCfg.Providers.Ollama.BaseURL,
					Logger:  opts.Logger,
				},
			)
		}

		addRoutes := func(category string, routeList []config.LLMRoute) {
			if len(routeList) == 0 {
				return
			}
			var rules []routeRule
			for _, route := range routeList {
				rules = append(rules, routeRule{Provider: route.Provider, Model: route.Model})
			}
			routes[category] = rules
		}

		addRoutes("fast", llmCfg.Categories.Fast)
		addRoutes("reasoning", llmCfg.Categories.Reasoning)
		addRoutes("local", llmCfg.Categories.Local)
		addRoutes("vision", llmCfg.Categories.Vision)
		addRoutes("embeddings", llmCfg.Categories.Embeddings)
	}

	return &Service{
		providers:       providerSet,
		defaultProvider: defaultProvider,
		routes:          routes,
		log:             opts.Logger.With().Str("component", "mcp_llm").Logger(),
	}
}

func (s *Service) NewServer() *server.MCPServer {
	mcpServer := server.NewMCPServer(
		"clara-llm",
		"0.1.0",
		server.WithToolCapabilities(true),
		server.WithInstructions(Description),
	)

	mcpServer.AddTool(mcp.NewTool(
		"generate",
		mcp.WithDescription("Generate text from a prompt using the configured LLM provider."),
		mcp.WithString("prompt", mcp.Required(), mcp.Description("Prompt text to send to the model.")),
		mcp.WithString("system", mcp.Description("Optional system instruction for the model.")),
		mcp.WithString(
			"category",
			mcp.Description("Optional routing category such as fast, reasoning, local, vision, or embeddings."),
		),
		mcp.WithString("provider", mcp.Description("Optional provider override, for example gemini.")),
		mcp.WithString("model", mcp.Description("Optional model override.")),
		mcp.WithNumber("temperature", mcp.Description("Optional temperature override.")),
		mcp.WithNumber(
			"max_output_tokens",
			mcp.Description("Optional maximum number of output tokens."),
		),
		mcp.WithString(
			"decode",
			mcp.Description("Optional decoding mode, e.g. json, to parse the response into structured output."),
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
			mcp.Description("Optional routing category such as fast, reasoning, local, vision, or embeddings."),
		),
		mcp.WithString("provider", mcp.Description("Optional provider override, for example gemini.")),
		mcp.WithString("model", mcp.Description("Optional model override.")),
		mcp.WithNumber("temperature", mcp.Description("Optional temperature override.")),
		mcp.WithNumber(
			"max_output_tokens",
			mcp.Description("Optional maximum number of output tokens."),
		),
		mcp.WithString(
			"decode",
			mcp.Description("Optional decoding mode, e.g. json, to parse the response into structured output."),
		),
	), s.handleGenerateVision)

	mcpServer.AddTool(mcp.NewTool(
		"embed",
		mcp.WithDescription("Generate embeddings for one string or a batch of strings."),
		mcp.WithString("input", mcp.Description("Single input string to embed.")),
		mcp.WithArray("inputs", mcp.Description("Array of strings to embed in one request.")),
		mcp.WithString("category", mcp.Description("Optional routing category, defaults to embeddings.")),
		mcp.WithString("provider", mcp.Description("Optional provider override.")),
		mcp.WithString("model", mcp.Description("Optional model override.")),
	), s.handleEmbed)

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

	rules, err := s.resolveProviders(call.Category, call.Provider, call.Model)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var lastErr error
	for _, rule := range rules {
		provider, ok := s.providers[rule.Provider]
		if !ok {
			lastErr = errors.Newf("unknown llm provider %q", rule.Provider)
			continue
		}

		callCopy := call
		if callCopy.Model == "" {
			callCopy.Model = rule.Model
		}

		result, err := provider.Generate(ctx, callCopy)
		if err == nil {
			if callCopy.Decode == "json" {
				result.Parsed = providers.DecodeJSON(result.Text)
			}
			return mcp.NewToolResultStructuredOnly(result), nil
		}

		s.log.Warn().Str("provider", rule.Provider).Err(err).Msg("generation failed, falling back")
		lastErr = err
	}

	return mcp.NewToolResultError(fmt.Sprintf("all providers failed. last error: %v", lastErr)), nil
}

func (s *Service) handleGenerateVision(
	ctx context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	call, err := parseGenerateRequest(req.GetArguments(), true)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	rules, err := s.resolveProviders(call.Category, call.Provider, call.Model)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var lastErr error
	for _, rule := range rules {
		provider, ok := s.providers[rule.Provider]
		if !ok {
			lastErr = errors.Newf("unknown llm provider %q", rule.Provider)
			continue
		}
		if !provider.Capabilities().Vision {
			lastErr = errors.Newf("provider %q does not support vision generation", provider.Name())
			continue
		}

		callCopy := call
		if callCopy.Model == "" {
			callCopy.Model = rule.Model
		}

		result, err := provider.GenerateVision(ctx, callCopy)
		if err == nil {
			if callCopy.Decode == "json" {
				result.Parsed = providers.DecodeJSON(result.Text)
			}
			return mcp.NewToolResultStructuredOnly(result), nil
		}

		s.log.Warn().Str("provider", rule.Provider).Err(err).Msg("vision generation failed, falling back")
		lastErr = err
	}

	return mcp.NewToolResultError(fmt.Sprintf("all providers failed. last error: %v", lastErr)), nil
}

func (s *Service) handleEmbed(
	ctx context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	call, err := parseEmbedRequest(req.GetArguments())
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	cat := call.Category
	if cat == "" {
		cat = "embeddings"
	}

	rules, err := s.resolveProviders(cat, call.Provider, call.Model)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var lastErr error
	for _, rule := range rules {
		provider, ok := s.providers[rule.Provider]
		if !ok {
			lastErr = errors.Newf("unknown llm provider %q", rule.Provider)
			continue
		}
		if !provider.Capabilities().Embed {
			lastErr = errors.Newf("provider %q does not support embeddings", provider.Name())
			continue
		}

		callCopy := call
		if callCopy.Model == "" {
			callCopy.Model = rule.Model
		}

		result, err := provider.Embed(ctx, callCopy)
		if err == nil {
			ret := map[string]any{
				"model":         result.ModelUsed,
				"provider_used": result.ProviderUsed,
				"count":         len(result.Embeddings),
				"embeddings":    result.Embeddings,
			}
			if len(result.Embeddings) == 1 {
				ret["embedding"] = result.Embeddings[0]
			}
			return mcp.NewToolResultStructuredOnly(ret), nil
		}

		s.log.Warn().Str("provider", rule.Provider).Err(err).Msg("embedding failed, falling back")
		lastErr = err
	}

	return mcp.NewToolResultError(fmt.Sprintf("all providers failed. last error: %v", lastErr)), nil
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

func (s *Service) resolveProviders(category, requestedProvider, requestedModel string) ([]routeRule, error) {
	requestedProvider = strings.TrimSpace(requestedProvider)
	if requestedProvider != "" {
		return []routeRule{{Provider: requestedProvider, Model: strings.TrimSpace(requestedModel)}}, nil
	}

	category = strings.TrimSpace(category)
	if rules, ok := s.routes[category]; ok && len(rules) > 0 {
		return rules, nil
	}

	return []routeRule{{Provider: s.defaultProvider, Model: strings.TrimSpace(requestedModel)}}, nil
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
		Decode:   stringArg(args, "decode"),
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

func parseEmbedRequest(args map[string]any) (providers.EmbedRequest, error) {
	var inputs []string

	if input, ok := args["input"]; ok {
		text, ok := input.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return providers.EmbedRequest{}, errors.New("embed.input must be a non-empty string")
		}
		if _, alsoBatch := args["inputs"]; alsoBatch {
			return providers.EmbedRequest{}, errors.New("provide either input or inputs, not both")
		}
		inputs = []string{text}
	} else if rawInputs, ok := args["inputs"]; ok {
		items, ok := rawInputs.([]any)
		if !ok || len(items) == 0 {
			return providers.EmbedRequest{}, errors.New("embed.inputs must be a non-empty array of strings")
		}
		inputs = make([]string, 0, len(items))
		for _, item := range items {
			text, ok := item.(string)
			if !ok || strings.TrimSpace(text) == "" {
				return providers.EmbedRequest{}, errors.New("embed.inputs must contain only non-empty strings")
			}
			inputs = append(inputs, text)
		}
	} else {
		return providers.EmbedRequest{}, errors.New("embed requires input or inputs")
	}

	return providers.EmbedRequest{
		Inputs:   inputs,
		Category: stringArg(args, "category"),
		Provider: stringArg(args, "provider"),
		Model:    stringArg(args, "model"),
	}, nil
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
