package ollamaembeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	Description  = "Built-in Ollama embeddings MCP server for local text embeddings."
	DefaultModel = "nomic-embed-text"
	DefaultURL   = "http://localhost:11434"
)

type Service struct {
	baseURL    string
	model      string
	httpClient *http.Client
}

func New(baseURL, model string) *Service {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = DefaultURL
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = DefaultModel
	}
	return &Service{
		baseURL: baseURL,
		model:   model,
		httpClient: &http.Client{
			Timeout: 2 * time.Minute,
		},
	}
}

func (s *Service) NewServer() *server.MCPServer {
	mcpServer := server.NewMCPServer(
		"clara-ollama-embeddings",
		"0.1.0",
		server.WithToolCapabilities(true),
	)

	mcpServer.AddTool(mcp.NewTool(
		"embed",
		mcp.WithDescription("Generate embeddings for one string or a batch of strings using Ollama."),
		mcp.WithString("input", mcp.Description("Single input string to embed.")),
		mcp.WithArray("inputs", mcp.Description("Array of strings to embed in one request when supported.")),
	), s.handleEmbed)

	return mcpServer
}

func (s *Service) handleEmbed(
	ctx context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	inputs, err := embedInputs(req.GetArguments())
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	embeddings, err := s.embed(ctx, inputs)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result := map[string]any{
		"model":      s.model,
		"url":        s.baseURL,
		"count":      len(embeddings),
		"embeddings": embeddings,
	}
	if len(embeddings) == 1 {
		result["embedding"] = embeddings[0]
	}
	return mcp.NewToolResultStructuredOnly(result), nil
}

func embedInputs(args map[string]any) ([]string, error) {
	if input, ok := args["input"]; ok {
		text, ok := input.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return nil, errors.New("embed.input must be a non-empty string")
		}
		if _, alsoBatch := args["inputs"]; alsoBatch {
			return nil, errors.New("provide either input or inputs, not both")
		}
		return []string{text}, nil
	}

	rawInputs, ok := args["inputs"]
	if !ok {
		return nil, errors.New("embed requires input or inputs")
	}
	items, ok := rawInputs.([]any)
	if !ok || len(items) == 0 {
		return nil, errors.New("embed.inputs must be a non-empty array of strings")
	}

	inputs := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return nil, errors.New("embed.inputs must contain only non-empty strings")
		}
		inputs = append(inputs, text)
	}
	return inputs, nil
}

func (s *Service) embed(ctx context.Context, inputs []string) ([][]float64, error) {
	embeddings, err := s.embedViaAPIEmbed(ctx, inputs)
	if err == nil {
		return embeddings, nil
	}

	var httpErr *httpStatusError
	if !errors.As(err, &httpErr) || httpErr.statusCode != http.StatusNotFound {
		return nil, err
	}

	return s.embedViaLegacyAPI(ctx, inputs)
}

func (s *Service) embedViaAPIEmbed(ctx context.Context, inputs []string) ([][]float64, error) {
	body, err := json.Marshal(map[string]any{
		"model": s.model,
		"input": inputPayload(inputs),
	})
	if err != nil {
		return nil, errors.Wrap(err, "marshal ollama embed request")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, errors.Wrap(err, "create ollama embed request")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "call ollama /api/embed")
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newHTTPStatusError(resp)
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

func (s *Service) embedViaLegacyAPI(ctx context.Context, inputs []string) ([][]float64, error) {
	embeddings := make([][]float64, 0, len(inputs))
	for _, input := range inputs {
		body, err := json.Marshal(map[string]any{
			"model":  s.model,
			"prompt": input,
		})
		if err != nil {
			return nil, errors.Wrap(err, "marshal ollama legacy embeddings request")
		}

		req, err := http.NewRequestWithContext(
			ctx,
			http.MethodPost,
			s.baseURL+"/api/embeddings",
			bytes.NewReader(body),
		)
		if err != nil {
			return nil, errors.Wrap(err, "create ollama legacy embeddings request")
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := s.httpClient.Do(req)
		if err != nil {
			return nil, errors.Wrap(err, "call ollama /api/embeddings")
		}

		func() {
			defer resp.Body.Close()
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				err = newHTTPStatusError(resp)
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

func inputPayload(inputs []string) any {
	if len(inputs) == 1 {
		return inputs[0]
	}
	payload := make([]string, len(inputs))
	copy(payload, inputs)
	return payload
}

type httpStatusError struct {
	statusCode int
	body       string
}

func (e *httpStatusError) Error() string {
	if e.body == "" {
		return fmt.Sprintf("ollama request failed with HTTP %d", e.statusCode)
	}
	return fmt.Sprintf("ollama request failed with HTTP %d: %s", e.statusCode, e.body)
}

func newHTTPStatusError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return &httpStatusError{
		statusCode: resp.StatusCode,
		body:       strings.TrimSpace(string(body)),
	}
}
