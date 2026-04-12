package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"
)

const (
	ProviderOllama       = "ollama"
	DefaultOllamaBaseURL = "http://localhost:11434"
)

type OllamaOptions struct {
	BaseURL    string
	HTTPClient *http.Client
	Logger     zerolog.Logger
}

type OllamaProvider struct {
	baseURL    string
	httpClient *http.Client
	log        zerolog.Logger

	genMu sync.Mutex
}

func NewOllama(opts OllamaOptions) *OllamaProvider {
	baseURL := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultOllamaBaseURL
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Minute}
	}

	return &OllamaProvider{
		baseURL:    baseURL,
		httpClient: httpClient,
		log:        opts.Logger.With().Str("provider", "ollama").Logger(),
	}
}

func (p *OllamaProvider) Name() string {
	return ProviderOllama
}

func (p *OllamaProvider) Capabilities() Capabilities {
	return Capabilities{Generate: true, Vision: false, Embed: true}
}

func (p *OllamaProvider) Status(_ context.Context) Status {
	return Status{
		Provider:     p.Name(),
		Status:       "available",
		Capabilities: p.Capabilities(),
	}
}

func (p *OllamaProvider) Generate(
	ctx context.Context,
	req GenerateRequest,
) (GenerateResult, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		return GenerateResult{}, errors.New("ollama requires a model to be specified in the routing config or request")
	}

	p.genMu.Lock()
	defer p.genMu.Unlock()

	response, err := p.generate(ctx, model, req.Prompt, req.System, false, nil)
	if err != nil {
		return GenerateResult{}, err
	}

	return GenerateResult{
		Text:         response,
		ProviderUsed: p.Name(),
		ModelUsed:    model,
		Parsed:       DecodeJSON(response),
	}, nil
}

func (p *OllamaProvider) GenerateVision(
	ctx context.Context,
	req GenerateRequest,
) (GenerateResult, error) {
	return GenerateResult{}, errors.New("ollama vision generation is not yet supported in this MCP server")
}

func (p *OllamaProvider) Embed(
	ctx context.Context,
	req EmbedRequest,
) (EmbedResult, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		return EmbedResult{}, errors.New("ollama embed requires a model to be specified")
	}

	embeddings, err := p.embed(ctx, model, req.Inputs)
	if err != nil {
		return EmbedResult{}, err
	}

	return EmbedResult{
		Embeddings:   embeddings,
		ProviderUsed: p.Name(),
		ModelUsed:    model,
	}, nil
}

func (p *OllamaProvider) embed(ctx context.Context, model string, inputs []string) ([][]float64, error) {
	embeddings, err := p.embedViaAPIEmbed(ctx, model, inputs)
	if err == nil {
		return embeddings, nil
	}

	var httpErr *ollamaHTTPStatusError
	if !errors.As(err, &httpErr) || httpErr.statusCode != http.StatusNotFound {
		return nil, err
	}

	return p.embedViaLegacyAPI(ctx, model, inputs)
}

func (p *OllamaProvider) embedViaAPIEmbed(
	ctx context.Context,
	model string,
	inputs []string,
) ([][]float64, error) {
	body, err := json.Marshal(map[string]any{
		"model": model,
		"input": inputPayload(inputs),
	})
	if err != nil {
		return nil, errors.Wrap(err, "marshal ollama embed request")
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		p.baseURL+"/api/embed",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, errors.Wrap(err, "create ollama embed request")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "call ollama /api/embed")
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newOllamaHTTPStatusError(resp)
	}

	var payload struct {
		Embeddings [][]float64 `json:"embeddings"`
		Embedding  []float64   `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, errors.Wrap(err, "decode ollama embed response")
	}
	switch {
	case len(payload.Embeddings) > 0:
		return payload.Embeddings, nil
	case len(payload.Embedding) > 0:
		return [][]float64{payload.Embedding}, nil
	default:
		return nil, errors.New("ollama embed response did not include embeddings")
	}
}

func (p *OllamaProvider) embedViaLegacyAPI(
	ctx context.Context,
	model string,
	inputs []string,
) ([][]float64, error) {
	embeddings := make([][]float64, 0, len(inputs))
	for _, input := range inputs {
		body, err := json.Marshal(map[string]any{
			"model":  model,
			"prompt": input,
		})
		if err != nil {
			return nil, errors.Wrap(err, "marshal ollama legacy embeddings request")
		}

		req, err := http.NewRequestWithContext(
			ctx,
			http.MethodPost,
			p.baseURL+"/api/embeddings",
			bytes.NewReader(body),
		)
		if err != nil {
			return nil, errors.Wrap(err, "create ollama legacy embeddings request")
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := p.httpClient.Do(req)
		if err != nil {
			return nil, errors.Wrap(err, "call ollama /api/embeddings")
		}

		func() {
			defer resp.Body.Close()
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				err = newOllamaHTTPStatusError(resp)
				return
			}

			var payload struct {
				Embedding []float64 `json:"embedding"`
			}
			if decodeErr := json.NewDecoder(resp.Body).Decode(&payload); decodeErr != nil {
				err = errors.Wrap(decodeErr, "decode ollama legacy embeddings response")
				return
			}
			if len(payload.Embedding) == 0 {
				err = errors.New("ollama legacy embeddings response did not include embedding")
				return
			}
			embeddings = append(embeddings, payload.Embedding)
		}()
		if err != nil {
			return nil, err
		}
	}
	return embeddings, nil
}

func (p *OllamaProvider) generate(
	ctx context.Context,
	model, prompt, system string,
	stream bool,
	options map[string]any,
) (string, error) {
	payload := map[string]any{
		"model":  model,
		"prompt": prompt,
		"stream": stream,
	}
	if system != "" {
		payload["system"] = system
	}
	if len(options) > 0 {
		payload["options"] = options
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", errors.Wrap(err, "marshal ollama generate request")
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		p.baseURL+"/api/generate",
		bytes.NewReader(body),
	)
	if err != nil {
		return "", errors.Wrap(err, "create ollama generate request")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		// Detect simple network failures/connection refused and convert to standard errors so it can fallback without RateLimitError syntax
		return "", errors.Wrap(err, "call ollama /api/generate")
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", newOllamaHTTPStatusError(resp)
	}

	if stream {
		return "", errors.New("streaming generate is not yet supported in this MCP server")
	}

	var result struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", errors.Wrap(err, "decode ollama generate response")
	}

	return result.Response, nil
}

func inputPayload(inputs []string) any {
	if len(inputs) == 1 {
		return inputs[0]
	}
	payload := make([]string, len(inputs))
	copy(payload, inputs)
	return payload
}

type ollamaHTTPStatusError struct {
	statusCode int
	body       string
}

func (e *ollamaHTTPStatusError) Error() string {
	if e.body == "" {
		return fmt.Sprintf("ollama request failed with HTTP %d", e.statusCode)
	}
	return fmt.Sprintf("ollama request failed with HTTP %d: %s", e.statusCode, e.body)
}

func newOllamaHTTPStatusError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return &ollamaHTTPStatusError{
		statusCode: resp.StatusCode,
		body:       strings.TrimSpace(string(body)),
	}
}
