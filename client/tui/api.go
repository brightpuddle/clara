package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Suggestion mirrors the db.Suggestion struct for JSON unmarshalling.
type Suggestion struct {
	ID          int64     `json:"ID"`
	Type        string    `json:"Type"`
	SourceDocID string    `json:"SourceDocID"`
	TargetDocID string    `json:"TargetDocID"`
	SourcePath  string    `json:"SourcePath"`
	TargetTitle string    `json:"TargetTitle"`
	Similarity  float64   `json:"Similarity"`
	Context     string    `json:"Context"`
	Status      string    `json:"Status"`
	CreatedAt   time.Time `json:"CreatedAt"`
}

// APIClient speaks to the clara-server REST API.
type APIClient struct {
	baseURL string
	http    *http.Client
}

func NewAPIClient(baseURL string) *APIClient {
	return &APIClient{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *APIClient) ListSuggestions(ctx context.Context) ([]Suggestion, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/v1/suggestions?status=pending", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list suggestions: %w", err)
	}
	defer resp.Body.Close()

	var suggestions []Suggestion
	if err := json.NewDecoder(resp.Body).Decode(&suggestions); err != nil {
		return nil, fmt.Errorf("decode suggestions: %w", err)
	}
	return suggestions, nil
}

func (c *APIClient) Approve(ctx context.Context, id int64) error {
	return c.postAction(ctx, fmt.Sprintf("/api/v1/suggestions/%d/approve", id))
}

func (c *APIClient) Reject(ctx context.Context, id int64) error {
	return c.postAction(ctx, fmt.Sprintf("/api/v1/suggestions/%d/reject", id))
}

func (c *APIClient) postAction(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
