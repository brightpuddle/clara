package providers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
)

const (
	DefaultGeminiModel   = "gemini-2.5-flash"
	DefaultGeminiBaseURL = "https://generativelanguage.googleapis.com"
)

type GeminiOptions struct {
	APIKey     string
	Model      string
	BaseURL    string
	HTTPClient *http.Client
}

type GeminiProvider struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

func NewGemini(opts GeminiOptions) *GeminiProvider {
	model := strings.TrimSpace(opts.Model)
	if model == "" {
		model = DefaultGeminiModel
	}

	baseURL := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultGeminiBaseURL
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Minute}
	}

	return &GeminiProvider{
		apiKey:     strings.TrimSpace(opts.APIKey),
		model:      model,
		baseURL:    baseURL,
		httpClient: httpClient,
	}
}

func (p *GeminiProvider) Name() string {
	return ProviderGemini
}

func (p *GeminiProvider) Capabilities() Capabilities {
	return Capabilities{Generate: true, Vision: true}
}

func (p *GeminiProvider) Status(_ context.Context) Status {
	status := Status{
		Provider:     p.Name(),
		Models:       []string{p.model},
		Capabilities: p.Capabilities(),
	}
	if p.apiKey == "" {
		status.Status = "unconfigured"
		status.Message = "missing Gemini API key"
		return status
	}
	status.Status = "available"
	return status
}

func (p *GeminiProvider) Generate(
	ctx context.Context,
	req GenerateRequest,
) (GenerateResult, error) {
	return p.generateContent(ctx, req, false)
}

func (p *GeminiProvider) GenerateVision(
	ctx context.Context,
	req GenerateRequest,
) (GenerateResult, error) {
	if len(req.Images) == 0 {
		return GenerateResult{}, errors.New("gemini vision generation requires at least one image")
	}
	return p.generateContent(ctx, req, true)
}

func (p *GeminiProvider) generateContent(
	ctx context.Context,
	req GenerateRequest,
	includeImages bool,
) (GenerateResult, error) {
	if p.apiKey == "" {
		return GenerateResult{}, errors.New("gemini provider is not configured; set GEMINI_API_KEY")
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = p.model
	}

	body, err := p.buildRequestBody(req, includeImages)
	if err != nil {
		return GenerateResult{}, err
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return GenerateResult{}, errors.Wrap(err, "marshal gemini request")
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", p.baseURL, model, p.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return GenerateResult{}, errors.Wrap(err, "create gemini request")
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return GenerateResult{}, errors.Wrap(err, "call gemini generateContent")
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return GenerateResult{}, newGeminiHTTPStatusError(resp)
	}

	var payload geminiGenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return GenerateResult{}, errors.Wrap(err, "decode gemini response")
	}

	if len(payload.Candidates) == 0 {
		if payload.PromptFeedback.BlockReason != "" {
			return GenerateResult{}, errors.Newf(
				"gemini blocked prompt: %s",
				payload.PromptFeedback.BlockReason,
			)
		}
		return GenerateResult{}, errors.New("gemini response did not include any candidates")
	}

	candidate := payload.Candidates[0]
	text := extractGeminiText(candidate.Content.Parts)
	if strings.TrimSpace(text) == "" {
		return GenerateResult{}, errors.New("gemini response did not include text content")
	}

	return GenerateResult{
		Text:         text,
		ProviderUsed: p.Name(),
		ModelUsed:    model,
		FinishReason: candidate.FinishReason,
		Parsed:       parseJSONObject(text),
	}, nil
}

func (p *GeminiProvider) buildRequestBody(
	req GenerateRequest,
	includeImages bool,
) (geminiGenerateRequest, error) {
	parts := []geminiPart{{Text: req.Prompt}}
	if includeImages {
		imageParts, err := imageParts(req.Images)
		if err != nil {
			return geminiGenerateRequest{}, err
		}
		parts = append(parts, imageParts...)
	}

	body := geminiGenerateRequest{
		Contents: []geminiContent{{Role: "user", Parts: parts}},
	}
	if req.System != "" {
		body.SystemInstruction = &geminiSystemInstruction{
			Parts: []geminiPart{{Text: req.System}},
		}
	}
	if req.Temperature != nil || req.MaxOutputTokens != nil {
		body.GenerationConfig = &geminiGenerationConfig{}
		if req.Temperature != nil {
			body.GenerationConfig.Temperature = req.Temperature
		}
		if req.MaxOutputTokens != nil {
			body.GenerationConfig.MaxOutputTokens = req.MaxOutputTokens
		}
	}

	return body, nil
}

func imageParts(paths []string) ([]geminiPart, error) {
	parts := make([]geminiPart, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, errors.Wrapf(err, "read image %q", path)
		}
		mimeType := detectImageMIMEType(path)
		parts = append(parts, geminiPart{
			InlineData: &geminiInlineData{
				MIMEType: mimeType,
				Data:     base64.StdEncoding.EncodeToString(data),
			},
		})
	}
	return parts, nil
}

func detectImageMIMEType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if detected := mime.TypeByExtension(ext); detected != "" {
		return detected
	}

	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".heic":
		return "image/heic"
	case ".heif":
		return "image/heif"
	default:
		return "application/octet-stream"
	}
}

func extractGeminiText(parts []geminiPart) string {
	chunks := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part.Text) == "" {
			continue
		}
		chunks = append(chunks, part.Text)
	}
	return strings.TrimSpace(strings.Join(chunks, "\n"))
}

func parseJSONObject(text string) any {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}

	// Strip markdown code blocks if present (e.g. ```json\n{...}\n```)
	if strings.HasPrefix(trimmed, "```") {
		firstNewline := strings.Index(trimmed, "\n")
		lastBackticks := strings.LastIndex(trimmed, "```")
		if firstNewline != -1 && lastBackticks > firstNewline {
			trimmed = strings.TrimSpace(trimmed[firstNewline+1 : lastBackticks])
		}
	}

	if !(strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) {
		return nil
	}

	var value any
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return nil
	}
	return value
}

type geminiGenerateRequest struct {
	SystemInstruction *geminiSystemInstruction `json:"system_instruction,omitempty"`
	Contents          []geminiContent          `json:"contents"`
	GenerationConfig  *geminiGenerationConfig  `json:"generationConfig,omitempty"`
}

type geminiSystemInstruction struct {
	Parts []geminiPart `json:"parts"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text       string            `json:"text,omitempty"`
	InlineData *geminiInlineData `json:"inline_data,omitempty"`
}

type geminiInlineData struct {
	MIMEType string `json:"mime_type"`
	Data     string `json:"data"`
}

type geminiGenerationConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	MaxOutputTokens *int     `json:"maxOutputTokens,omitempty"`
}

type geminiGenerateResponse struct {
	Candidates     []geminiCandidate    `json:"candidates"`
	PromptFeedback geminiPromptFeedback `json:"promptFeedback"`
	Error          *geminiResponseError `json:"error,omitempty"`
}

type geminiCandidate struct {
	Content      geminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
}

type geminiPromptFeedback struct {
	BlockReason string `json:"blockReason"`
}

type geminiResponseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

type geminiHTTPStatusError struct {
	statusCode int
	body       string
}

func (e *geminiHTTPStatusError) Error() string {
	if e.body == "" {
		return fmt.Sprintf("gemini request failed with HTTP %d", e.statusCode)
	}
	return fmt.Sprintf("gemini request failed with HTTP %d: %s", e.statusCode, e.body)
}

func newGeminiHTTPStatusError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	trimmed := strings.TrimSpace(string(body))
	if trimmed != "" {
		var payload struct {
			Error *geminiResponseError `json:"error"`
		}
		if err := json.Unmarshal(body, &payload); err == nil && payload.Error != nil {
			trimmed = payload.Error.Message
		}
	}
	return &geminiHTTPStatusError{statusCode: resp.StatusCode, body: trimmed}
}
