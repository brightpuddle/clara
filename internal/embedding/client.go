// Package embedding provides an Ollama API client for generating text embeddings.
package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/cockroachdb/errors"
)

const defaultTimeout = 30 * time.Second

// Client is an HTTP client for the Ollama embeddings API.
type Client struct {
	baseURL    string
	model      string
	httpClient *http.Client
}

// New creates a new embedding Client.
func New(baseURL, model string) *Client {
	return &Client{
		baseURL: baseURL,
		model:   model,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

// Embed generates an embedding vector for the given text.
// Returns a []float32 with 768 dimensions (nomic-embed-text).
func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	reqBody, err := json.Marshal(map[string]string{
		"model":  c.model,
		"prompt": text,
	})
	if err != nil {
		return nil, errors.Wrap(err, "marshal embed request")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/embeddings", bytes.NewReader(reqBody))
	if err != nil {
		return nil, errors.Wrap(err, "create embed request")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "ollama embed request")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "read embed response")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("ollama embed: status %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, errors.Wrap(err, "unmarshal embed response")
	}
	if len(result.Embedding) == 0 {
		return nil, errors.New("ollama returned empty embedding")
	}
	return result.Embedding, nil
}
