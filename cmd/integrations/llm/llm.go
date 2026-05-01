package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/brightpuddle/clara/pkg/contract"
	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/mcp"
)

type Config struct {
	Categories map[string][]ModelConfig  `json:"categories"`
	Providers  map[string]ProviderConfig `json:"providers"`
}

type ModelConfig struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type ProviderConfig struct {
	APIKey  string `json:"api_key,omitempty"`
	BaseURL string `json:"base_url,omitempty"`
	Key     string `json:"key,omitempty"` // For OpenAI compatible
}

type LLMPlugin struct {
	config Config
}

func (p *LLMPlugin) Configure(config []byte) error {
	if err := json.Unmarshal(config, &p.config); err != nil {
		return errors.Wrap(err, "unmarshal llm config")
	}
	return nil
}

func (p *LLMPlugin) Description() (string, error) {
	return "LLM integration providing generation and embeddings via multiple providers", nil
}

func (p *LLMPlugin) Tools() ([]byte, error) {
	tools := []mcp.Tool{
		mcp.NewTool(
			"generate",
			mcp.WithDescription("Get a generation from an LLM category (fast, reasoning, local)"),
			mcp.WithString(
				"category",
				mcp.Required(),
				mcp.Description("Category of LLM to use (e.g. fast, reasoning, local)"),
			),
			mcp.WithArray(
				"messages",
				mcp.Required(),
				mcp.Description("Conversation history as an array of {role, content} objects"),
			),
			mcp.WithNumber("temperature", mcp.Description("Sampling temperature")),
			mcp.WithNumber("max_tokens", mcp.Description("Maximum tokens to generate")),
		),
		mcp.NewTool(
			"generate_vision",
			mcp.WithDescription("Get a vision-based generation from an LLM category (vision)"),
			mcp.WithString(
				"category",
				mcp.Required(),
				mcp.Description("Category of LLM to use (e.g. vision)"),
			),
			mcp.WithArray(
				"messages",
				mcp.Required(),
				mcp.Description("Conversation history as an array of {role, content} objects"),
			),
			mcp.WithString(
				"image_url",
				mcp.Description("URL or data:image/... of the image to analyze"),
			),
			mcp.WithString("image_base64", mcp.Description("Raw base64 data of the image")),
			mcp.WithNumber("temperature", mcp.Description("Sampling temperature")),
			mcp.WithNumber("max_tokens", mcp.Description("Maximum tokens to generate")),
		),
		mcp.NewTool(
			"embed",
			mcp.WithDescription("Get embeddings for one or more strings"),
			mcp.WithString(
				"category",
				mcp.Required(),
				mcp.Description("Category of LLM to use (e.g. embeddings)"),
			),
			mcp.WithArray("input", mcp.Required(), mcp.Description("Array of strings to embed")),
		),
	}
	return json.Marshal(tools)
}

func (p *LLMPlugin) CallTool(name string, args []byte) ([]byte, error) {
	switch name {
	case "generate":
		var req struct {
			Category    string             `json:"category"`
			Messages    []contract.Message `json:"messages"`
			Temperature float32            `json:"temperature"`
			MaxTokens   int                `json:"max_tokens"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, err
		}
		resp, err := p.Generate(req.Category, contract.GenerateRequest{
			Messages:    req.Messages,
			Temperature: req.Temperature,
			MaxTokens:   req.MaxTokens,
		})
		if err != nil {
			return nil, err
		}
		return json.Marshal(resp)

	case "generate_vision":
		var req struct {
			Category    string             `json:"category"`
			Messages    []contract.Message `json:"messages"`
			ImageURL    string             `json:"image_url"`
			ImageBase64 string             `json:"image_base64"`
			Temperature float32            `json:"temperature"`
			MaxTokens   int                `json:"max_tokens"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, err
		}
		resp, err := p.GenerateVision(req.Category, contract.VisionRequest{
			Messages:    req.Messages,
			ImageURL:    req.ImageURL,
			ImageBase64: req.ImageBase64,
			Temperature: req.Temperature,
			MaxTokens:   req.MaxTokens,
		})
		if err != nil {
			return nil, err
		}
		return json.Marshal(resp)

	case "embed":
		var req struct {
			Category string   `json:"category"`
			Input    []string `json:"input"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, err
		}
		resp, err := p.Embed(req.Category, req.Input)
		if err != nil {
			return nil, err
		}
		return json.Marshal(resp)

	default:
		return nil, fmt.Errorf("tool not found: %s", name)
	}
}

func (p *LLMPlugin) Generate(
	category string,
	req contract.GenerateRequest,
) (contract.GenerateResponse, error) {
	models, ok := p.config.Categories[category]
	if !ok || len(models) == 0 {
		return contract.GenerateResponse{}, fmt.Errorf(
			"no models configured for category: %s",
			category,
		)
	}

	// Try each model in the category
	var lastErr error
	for _, m := range models {
		provider, ok := p.config.Providers[m.Provider]
		if !ok {
			lastErr = fmt.Errorf("provider not configured: %s", m.Provider)
			continue
		}

		resp, err := p.callProviderGenerate(m.Provider, provider, m.Model, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}

	return contract.GenerateResponse{}, errors.Wrapf(
		lastErr,
		"failed to generate via any model in category %s",
		category,
	)
}

func (p *LLMPlugin) GenerateVision(
	category string,
	req contract.VisionRequest,
) (contract.GenerateResponse, error) {
	models, ok := p.config.Categories[category]
	if !ok || len(models) == 0 {
		return contract.GenerateResponse{}, fmt.Errorf(
			"no models configured for category: %s",
			category,
		)
	}

	// Try each model in the category
	var lastErr error
	for _, m := range models {
		provider, ok := p.config.Providers[m.Provider]
		if !ok {
			lastErr = fmt.Errorf("provider not configured: %s", m.Provider)
			continue
		}

		resp, err := p.callProviderVision(m.Provider, provider, m.Model, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}

	return contract.GenerateResponse{}, errors.Wrapf(
		lastErr,
		"failed to generate vision via any model in category %s",
		category,
	)
}

func (p *LLMPlugin) Embed(category string, input []string) ([][]float32, error) {
	models, ok := p.config.Categories[category]
	if !ok || len(models) == 0 {
		return nil, fmt.Errorf("no models configured for category: %s", category)
	}

	// Try each model in the category
	var lastErr error
	for _, m := range models {
		provider, ok := p.config.Providers[m.Provider]
		if !ok {
			lastErr = fmt.Errorf("provider not configured: %s", m.Provider)
			continue
		}

		resp, err := p.callProviderEmbed(m.Provider, provider, m.Model, input)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}

	return nil, errors.Wrapf(lastErr, "failed to embed via any model in category %s", category)
}

func (p *LLMPlugin) callProviderGenerate(
	providerName string,
	cfg ProviderConfig,
	model string,
	req contract.GenerateRequest,
) (contract.GenerateResponse, error) {
	switch providerName {
	case "gemini":
		return p.callGeminiGenerate(cfg, model, req)
	case "ollama":
		return p.callOllamaGenerate(cfg, model, req)
	case "openai":
		return p.callOpenAIGenerate(cfg, model, req)
	default:
		return contract.GenerateResponse{}, fmt.Errorf("unsupported provider: %s", providerName)
	}
}

func (p *LLMPlugin) callProviderVision(
	providerName string,
	cfg ProviderConfig,
	model string,
	req contract.VisionRequest,
) (contract.GenerateResponse, error) {
	switch providerName {
	case "gemini":
		return p.callGeminiVision(cfg, model, req)
	case "ollama":
		// Ollama handles vision via chat API too
		return p.callOllamaVision(cfg, model, req)
	case "openai":
		return p.callOpenAIVision(cfg, model, req)
	default:
		return contract.GenerateResponse{}, fmt.Errorf(
			"unsupported provider for vision: %s",
			providerName,
		)
	}
}

func (p *LLMPlugin) callProviderEmbed(
	providerName string,
	cfg ProviderConfig,
	model string,
	input []string,
) ([][]float32, error) {
	switch providerName {
	case "gemini":
		return p.callGeminiEmbed(cfg, model, input)
	case "ollama":
		return p.callOllamaEmbed(cfg, model, input)
	case "openai":
		return p.callOpenAIEmbed(cfg, model, input)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", providerName)
	}
}

// --- Gemini Implementation ---

func (p *LLMPlugin) callGeminiGenerate(
	cfg ProviderConfig,
	model string,
	req contract.GenerateRequest,
) (contract.GenerateResponse, error) {
	if cfg.APIKey == "" {
		return contract.GenerateResponse{}, fmt.Errorf("gemini api_key not configured")
	}

	url := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		model,
		cfg.APIKey,
	)

	type Part struct {
		Text string `json:"text,omitempty"`
	}
	type Content struct {
		Role  string `json:"role"`
		Parts []Part `json:"parts"`
	}
	type GeminiReq struct {
		Contents         []Content `json:"contents"`
		GenerationConfig struct {
			Temperature     float32 `json:"temperature,omitempty"`
			MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
		} `json:"generationConfig,omitempty"`
	}

	var contents []Content
	for _, msg := range req.Messages {
		role := msg.Role
		if role == "assistant" {
			role = "model"
		}
		if role == "system" {
			role = "user"
		}
		contents = append(contents, Content{
			Role:  role,
			Parts: []Part{{Text: msg.Content}},
		})
	}

	geminiReq := GeminiReq{
		Contents: contents,
	}
	geminiReq.GenerationConfig.Temperature = req.Temperature
	geminiReq.GenerationConfig.MaxOutputTokens = req.MaxTokens

	jsonBody, _ := json.Marshal(geminiReq)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		return contract.GenerateResponse{}, errors.Wrap(err, "post to gemini")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return contract.GenerateResponse{}, fmt.Errorf(
			"gemini returned status %d: %s",
			resp.StatusCode,
			string(respBody),
		)
	}

	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []Part `json:"parts"`
				Role  string `json:"role"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return contract.GenerateResponse{}, errors.Wrap(err, "decode gemini response")
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return contract.GenerateResponse{}, fmt.Errorf("gemini returned no candidates")
	}

	return contract.GenerateResponse{
		Message: contract.Message{
			Role:    "assistant",
			Content: geminiResp.Candidates[0].Content.Parts[0].Text,
		},
	}, nil
}

func (p *LLMPlugin) callGeminiVision(
	cfg ProviderConfig,
	model string,
	req contract.VisionRequest,
) (contract.GenerateResponse, error) {
	// For Gemini vision, we need to handle the image part.
	// Gemini v1beta supports inlineData for base64.
	if cfg.APIKey == "" {
		return contract.GenerateResponse{}, fmt.Errorf("gemini api_key not configured")
	}

	url := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		model,
		cfg.APIKey,
	)

	type InlineData struct {
		MimeType string `json:"mime_type"`
		Data     string `json:"data"`
	}
	type Part struct {
		Text       string      `json:"text,omitempty"`
		InlineData *InlineData `json:"inline_data,omitempty"`
	}
	type Content struct {
		Role  string `json:"role"`
		Parts []Part `json:"parts"`
	}
	type GeminiReq struct {
		Contents         []Content `json:"contents"`
		GenerationConfig struct {
			Temperature     float32 `json:"temperature,omitempty"`
			MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
		} `json:"generationConfig,omitempty"`
	}

	var contents []Content
	for _, msg := range req.Messages {
		role := msg.Role
		if role == "assistant" {
			role = "model"
		}
		if role == "system" {
			role = "user"
		}
		contents = append(contents, Content{
			Role:  role,
			Parts: []Part{{Text: msg.Content}},
		})
	}

	// Add image to the last content if it's from user, otherwise create a new user content
	last := &contents[len(contents)-1]
	if last.Role != "user" {
		contents = append(contents, Content{Role: "user", Parts: []Part{}})
		last = &contents[len(contents)-1]
	}

	if req.ImageBase64 != "" {
		last.Parts = append(last.Parts, Part{
			InlineData: &InlineData{
				MimeType: "image/png", // Should ideally detect
				Data:     req.ImageBase64,
			},
		})
	} else if req.ImageURL != "" && strings.HasPrefix(req.ImageURL, "data:") {
		// Extract base64 from data URL
		parts := strings.SplitN(req.ImageURL, ",", 2)
		if len(parts) == 2 {
			mime := "image/png"
			if strings.Contains(parts[0], "image/jpeg") {
				mime = "image/jpeg"
			}
			last.Parts = append(last.Parts, Part{
				InlineData: &InlineData{
					MimeType: mime,
					Data:     parts[1],
				},
			})
		}
	}

	geminiReq := GeminiReq{
		Contents: contents,
	}
	geminiReq.GenerationConfig.Temperature = req.Temperature
	geminiReq.GenerationConfig.MaxOutputTokens = req.MaxTokens

	jsonBody, _ := json.Marshal(geminiReq)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		return contract.GenerateResponse{}, errors.Wrap(err, "post to gemini")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return contract.GenerateResponse{}, fmt.Errorf(
			"gemini vision returned status %d: %s",
			resp.StatusCode,
			string(respBody),
		)
	}

	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return contract.GenerateResponse{}, errors.Wrap(err, "decode gemini response")
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return contract.GenerateResponse{}, fmt.Errorf("gemini vision returned no candidates")
	}

	return contract.GenerateResponse{
		Message: contract.Message{
			Role:    "assistant",
			Content: geminiResp.Candidates[0].Content.Parts[0].Text,
		},
	}, nil
}

func (p *LLMPlugin) callGeminiEmbed(
	cfg ProviderConfig,
	model string,
	input []string,
) ([][]float32, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("gemini api_key not configured")
	}

	// Gemini embedding API is per-request, but there is a batchEmbedContents
	url := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/%s:batchEmbedContents?key=%s",
		model,
		cfg.APIKey,
	)

	type Content struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	}
	type EmbedRequest struct {
		Model   string  `json:"model"`
		Content Content `json:"content"`
	}
	type BatchEmbedRequest struct {
		Requests []EmbedRequest `json:"requests"`
	}

	var requests []EmbedRequest
	for _, text := range input {
		requests = append(requests, EmbedRequest{
			Model: "models/" + model,
			Content: Content{
				Parts: []struct {
					Text string `json:"text"`
				}{{Text: text}},
			},
		})
	}

	jsonBody, _ := json.Marshal(BatchEmbedRequest{Requests: requests})
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, errors.Wrap(err, "post to gemini")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var geminiResp struct {
		Embeddings []struct {
			Values []float32 `json:"values"`
		} `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return nil, errors.Wrap(err, "decode gemini response")
	}

	var res [][]float32
	for _, e := range geminiResp.Embeddings {
		res = append(res, e.Values)
	}
	return res, nil
}

// --- Ollama Implementation ---

func (p *LLMPlugin) callOllamaGenerate(
	cfg ProviderConfig,
	model string,
	req contract.GenerateRequest,
) (contract.GenerateResponse, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:11434"
	}

	url := fmt.Sprintf("%s/api/chat", strings.TrimSuffix(cfg.BaseURL, "/"))
	body := map[string]any{
		"model":    model,
		"messages": req.Messages,
		"stream":   false,
		"options": map[string]any{
			"temperature": req.Temperature,
		},
	}

	jsonBody, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		return contract.GenerateResponse{}, errors.Wrap(err, "post to ollama")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return contract.GenerateResponse{}, fmt.Errorf(
			"ollama returned status %d: %s",
			resp.StatusCode,
			string(respBody),
		)
	}

	var ollamaResp struct {
		Message contract.Message `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return contract.GenerateResponse{}, errors.Wrap(err, "decode ollama response")
	}

	return contract.GenerateResponse{Message: ollamaResp.Message}, nil
}

func (p *LLMPlugin) callOllamaVision(
	cfg ProviderConfig,
	model string,
	req contract.VisionRequest,
) (contract.GenerateResponse, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:11434"
	}

	url := fmt.Sprintf("%s/api/chat", strings.TrimSuffix(cfg.BaseURL, "/"))

	type OllamaMsg struct {
		Role    string   `json:"role"`
		Content string   `json:"content"`
		Images  []string `json:"images,omitempty"`
	}

	var messages []OllamaMsg
	for _, m := range req.Messages {
		messages = append(messages, OllamaMsg{Role: m.Role, Content: m.Content})
	}

	// Add image to last message
	if len(messages) > 0 {
		img := ""
		if req.ImageBase64 != "" {
			img = req.ImageBase64
		} else if req.ImageURL != "" && strings.HasPrefix(req.ImageURL, "data:") {
			parts := strings.SplitN(req.ImageURL, ",", 2)
			if len(parts) == 2 {
				img = parts[1]
			}
		}
		if img != "" {
			messages[len(messages)-1].Images = append(messages[len(messages)-1].Images, img)
		}
	}

	body := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   false,
		"options": map[string]any{
			"temperature": req.Temperature,
		},
	}

	jsonBody, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		return contract.GenerateResponse{}, errors.Wrap(err, "post to ollama vision")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return contract.GenerateResponse{}, fmt.Errorf(
			"ollama vision returned status %d: %s",
			resp.StatusCode,
			string(respBody),
		)
	}

	var ollamaResp struct {
		Message contract.Message `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return contract.GenerateResponse{}, errors.Wrap(err, "decode ollama response")
	}

	return contract.GenerateResponse{Message: ollamaResp.Message}, nil
}

func (p *LLMPlugin) callOllamaEmbed(
	cfg ProviderConfig,
	model string,
	input []string,
) ([][]float32, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:11434"
	}

	url := fmt.Sprintf("%s/api/embed", strings.TrimSuffix(cfg.BaseURL, "/"))
	body := map[string]any{
		"model": model,
		"input": input,
	}

	jsonBody, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, errors.Wrap(err, "post to ollama")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var ollamaResp struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return nil, errors.Wrap(err, "decode ollama response")
	}

	return ollamaResp.Embeddings, nil
}

// --- OpenAI Implementation ---

func (p *LLMPlugin) callOpenAIGenerate(
	cfg ProviderConfig,
	model string,
	req contract.GenerateRequest,
) (contract.GenerateResponse, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}

	url := fmt.Sprintf("%s/chat/completions", strings.TrimSuffix(cfg.BaseURL, "/"))
	body := map[string]any{
		"model":       model,
		"messages":    req.Messages,
		"temperature": req.Temperature,
		"max_tokens":  req.MaxTokens,
	}

	jsonBody, _ := json.Marshal(body)
	hReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return contract.GenerateResponse{}, errors.Wrap(err, "create openai request")
	}
	hReq.Header.Set("Content-Type", "application/json")
	if cfg.Key != "" {
		hReq.Header.Set("Authorization", "Bearer "+cfg.Key)
	}

	resp, err := http.DefaultClient.Do(hReq)
	if err != nil {
		return contract.GenerateResponse{}, errors.Wrap(err, "post to openai")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return contract.GenerateResponse{}, fmt.Errorf(
			"openai returned status %d: %s",
			resp.StatusCode,
			string(respBody),
		)
	}

	var openAIResp struct {
		Choices []struct {
			Message contract.Message `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&openAIResp); err != nil {
		return contract.GenerateResponse{}, errors.Wrap(err, "decode openai response")
	}

	if len(openAIResp.Choices) == 0 {
		return contract.GenerateResponse{}, fmt.Errorf("openai returned no choices")
	}

	return contract.GenerateResponse{Message: openAIResp.Choices[0].Message}, nil
}

func (p *LLMPlugin) callOpenAIVision(
	cfg ProviderConfig,
	model string,
	req contract.VisionRequest,
) (contract.GenerateResponse, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}

	url := fmt.Sprintf("%s/chat/completions", strings.TrimSuffix(cfg.BaseURL, "/"))

	type ImageURL struct {
		URL string `json:"url"`
	}
	type ContentPart struct {
		Type     string    `json:"type"`
		Text     string    `json:"text,omitempty"`
		ImageURL *ImageURL `json:"image_url,omitempty"`
	}
	type OpenAIMsg struct {
		Role    string        `json:"role"`
		Content []ContentPart `json:"content"`
	}

	var messages []OpenAIMsg
	for _, m := range req.Messages {
		messages = append(messages, OpenAIMsg{
			Role: m.Role,
			Content: []ContentPart{
				{Type: "text", Text: m.Content},
			},
		})
	}

	// Add image to last message
	if len(messages) > 0 {
		img := ""
		if req.ImageBase64 != "" {
			img = "data:image/png;base64," + req.ImageBase64
		} else if req.ImageURL != "" {
			img = req.ImageURL
		}
		if img != "" {
			messages[len(messages)-1].Content = append(
				messages[len(messages)-1].Content,
				ContentPart{
					Type:     "image_url",
					ImageURL: &ImageURL{URL: img},
				},
			)
		}
	}

	body := map[string]any{
		"model":       model,
		"messages":    messages,
		"temperature": req.Temperature,
		"max_tokens":  req.MaxTokens,
	}

	jsonBody, _ := json.Marshal(body)
	hReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return contract.GenerateResponse{}, errors.Wrap(err, "create openai vision request")
	}
	hReq.Header.Set("Content-Type", "application/json")
	if cfg.Key != "" {
		hReq.Header.Set("Authorization", "Bearer "+cfg.Key)
	}

	resp, err := http.DefaultClient.Do(hReq)
	if err != nil {
		return contract.GenerateResponse{}, errors.Wrap(err, "post to openai vision")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return contract.GenerateResponse{}, fmt.Errorf(
			"openai vision returned status %d: %s",
			resp.StatusCode,
			string(respBody),
		)
	}

	var openAIResp struct {
		Choices []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&openAIResp); err != nil {
		return contract.GenerateResponse{}, errors.Wrap(err, "decode openai response")
	}

	if len(openAIResp.Choices) == 0 {
		return contract.GenerateResponse{}, fmt.Errorf("openai vision returned no choices")
	}

	return contract.GenerateResponse{
		Message: contract.Message{
			Role:    openAIResp.Choices[0].Message.Role,
			Content: openAIResp.Choices[0].Message.Content,
		},
	}, nil
}

func (p *LLMPlugin) callOpenAIEmbed(
	cfg ProviderConfig,
	model string,
	input []string,
) ([][]float32, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}

	url := fmt.Sprintf("%s/embeddings", strings.TrimSuffix(cfg.BaseURL, "/"))
	body := map[string]any{
		"model": model,
		"input": input,
	}

	jsonBody, _ := json.Marshal(body)
	hReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, errors.Wrap(err, "create openai request")
	}
	hReq.Header.Set("Content-Type", "application/json")
	if cfg.Key != "" {
		hReq.Header.Set("Authorization", "Bearer "+cfg.Key)
	}

	resp, err := http.DefaultClient.Do(hReq)
	if err != nil {
		return nil, errors.Wrap(err, "post to openai")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var openAIResp struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&openAIResp); err != nil {
		return nil, errors.Wrap(err, "decode openai response")
	}

	var res [][]float32
	for _, d := range openAIResp.Data {
		res = append(res, d.Embedding)
	}
	return res, nil
}
