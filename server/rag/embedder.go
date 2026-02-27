package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const defaultModel = "nomic-embed-text"

// Embedder calls the Ollama HTTP API to generate embeddings.
type Embedder struct {
	baseURL string
	model   string
	client  *http.Client
}

func NewEmbedder(ollamaURL, model string) *Embedder {
	if model == "" {
		model = defaultModel
	}
	return &Embedder{
		baseURL: ollamaURL,
		model:   model,
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

type embedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type embedResponse struct {
	Embedding []float32 `json:"embedding"`
}

// Embed returns a 768-dimensional embedding for the given text.
func (e *Embedder) Embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(embedRequest{Model: e.model, Prompt: text})
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

	var result embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ollama embed decode: %w", err)
	}
	return result.Embedding, nil
}

// EmbedChunks embeds all chunks and returns the embeddings in order.
func (e *Embedder) EmbedChunks(ctx context.Context, chunks []string) ([][]float32, error) {
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
