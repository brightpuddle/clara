package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Embedder is the provider-agnostic interface for generating text embeddings.
// Implementations: OllamaEmbedder (default), OpenAIEmbedder (future).
type Embedder interface {
	// Embed returns a vector embedding for a single text string.
	Embed(ctx context.Context, text string) ([]float32, error)
	// EmbedChunks embeds a slice of texts and returns embeddings in order.
	EmbedChunks(ctx context.Context, chunks []string) ([][]float32, error)
	// Dimensions returns the length of vectors this provider produces.
	// Used to validate against the database schema (schema.sql: vector(768)).
	Dimensions() int
}

// embedChunks is a shared helper that calls Embed sequentially.
// Individual providers can override EmbedChunks for batch API efficiency.
func embedChunks(e Embedder, ctx context.Context, chunks []string) ([][]float32, error) {
	embeddings := make([][]float32, len(chunks))
	for i, chunk := range chunks {
		emb, err := e.Embed(ctx, chunk)
		if err != nil {
			return nil, fmt.Errorf("chunk %d: %w", i, err)
		}
		embeddings[i] = emb
	}
	return embeddings, nil
}

// ---- OllamaEmbedder ---------------------------------------------------------

const (
	defaultOllamaModel = "nomic-embed-text"
	ollamaDimensions   = 768 // nomic-embed-text output size
)

// OllamaEmbedder calls a locally-running Ollama instance via its HTTP API.
// Ollama must run natively on macOS to use Metal GPU acceleration — do NOT
// run it inside Docker/Podman on Apple Silicon (no GPU passthrough).
type OllamaEmbedder struct {
	baseURL string
	model   string
	client  *http.Client
}

func NewOllamaEmbedder(baseURL, model string) *OllamaEmbedder {
	if model == "" {
		model = defaultOllamaModel
	}
	return &OllamaEmbedder{
		baseURL: baseURL,
		model:   model,
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

func (e *OllamaEmbedder) Dimensions() int { return ollamaDimensions }

type ollamaEmbedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaEmbedResponse struct {
	Embedding []float32 `json:"embedding"`
}

func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(ollamaEmbedRequest{Model: e.model, Prompt: text})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		e.baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama embed: HTTP %d", resp.StatusCode)
	}

	var result ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ollama embed decode: %w", err)
	}
	return result.Embedding, nil
}

func (e *OllamaEmbedder) EmbedChunks(ctx context.Context, chunks []string) ([][]float32, error) {
	return embedChunks(e, ctx, chunks)
}

// ---- OpenAIEmbedder (future) ------------------------------------------------

// OpenAIEmbedder uses the OpenAI embeddings API (text-embedding-3-small et al.).
// Configure via server.yaml: ai.provider = "openai", ai.openai.api_key = "sk-..."
//
// NOTE: OpenAI text-embedding-3-small produces 1536-dim vectors by default.
// Use dimensions: 768 in the API request to match the current schema, or run
// a schema migration to widen the vector column before switching providers.
type OpenAIEmbedder struct {
	apiKey  string
	model   string
	baseURL string
	dims    int
	client  *http.Client
}

const (
	defaultOpenAIModel   = "text-embedding-3-small"
	defaultOpenAIBaseURL = "https://api.openai.com/v1"
	// Project to 768 dims to stay compatible with current schema without migration.
	openAIProjectedDims = 768
)

func NewOpenAIEmbedder(apiKey, model, baseURL string) *OpenAIEmbedder {
	if model == "" {
		model = defaultOpenAIModel
	}
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
	}
	return &OpenAIEmbedder{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		dims:    openAIProjectedDims,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (e *OpenAIEmbedder) Dimensions() int { return e.dims }

type openAIEmbedRequest struct {
	Model      string `json:"model"`
	Input      string `json:"input"`
	Dimensions int    `json:"dimensions,omitempty"`
}

type openAIEmbedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

func (e *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(openAIEmbedRequest{
		Model:      e.model,
		Input:      text,
		Dimensions: e.dims,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		e.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai embed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai embed: HTTP %d", resp.StatusCode)
	}

	var result openAIEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("openai embed decode: %w", err)
	}
	if len(result.Data) == 0 {
		return nil, fmt.Errorf("openai embed: empty response")
	}
	return result.Data[0].Embedding, nil
}

func (e *OpenAIEmbedder) EmbedChunks(ctx context.Context, chunks []string) ([][]float32, error) {
	// TODO: use the batch endpoint (/embeddings with input as []string) for efficiency.
	return embedChunks(e, ctx, chunks)
}

