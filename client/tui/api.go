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
	ID            int64     `json:"ID"`
	Type          string    `json:"Type"`
	SourceDocID   string    `json:"SourceDocID"`
	TargetDocID   string    `json:"TargetDocID"`
	SourcePath    string    `json:"SourcePath"`
	TargetTitle   string    `json:"TargetTitle"`
	Similarity    float64   `json:"Similarity"`
	Context       string    `json:"Context"`
	Status        string    `json:"Status"`
	ActionSurface string    `json:"ActionSurface"`
	CreatedAt     time.Time `json:"CreatedAt"`
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

// ServerStatus is the response from GET /api/v1/status.
type ServerStatus struct {
	Status      string `json:"status"`
	Uptime      string `json:"uptime"`
	Documents   int    `json:"documents"`
	Suggestions struct {
		Pending  int `json:"pending"`
		Approved int `json:"approved"`
		Rejected int `json:"rejected"`
	} `json:"suggestions"`
}

func (c *APIClient) GetStatus(ctx context.Context) (ServerStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/status", nil)
	if err != nil {
		return ServerStatus{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return ServerStatus{}, fmt.Errorf("server status: %w", err)
	}
	defer resp.Body.Close()
	var s ServerStatus
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return ServerStatus{}, fmt.Errorf("decode status: %w", err)
	}
	return s, nil
}

// GetProposals returns pending proposals from the server as ClaraItems.
func (c *APIClient) GetProposals(ctx context.Context) ([]ClaraItemJSON, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/proposals", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get proposals: %w", err)
	}
	defer resp.Body.Close()
	var items []ClaraItemJSON
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("decode proposals: %w", err)
	}
	return items, nil
}

// Dismiss marks a proposal as dismissed (softer than reject).
func (c *APIClient) Dismiss(ctx context.Context, id int64) error {
	return c.postAction(ctx, fmt.Sprintf("/api/v1/proposals/%d/dismiss", id))
}

// ClaraItemJSON is the JSON wire format for a ClaraItem from the proposals API.
type ClaraItemJSON struct {
	ID            string    `json:"id"`
	Type          string    `json:"type"`
	Source        string    `json:"source"`
	SourceRef     string    `json:"source_ref,omitempty"`
	Priority      string    `json:"priority,omitempty"`
	Tags          []string  `json:"tags,omitempty"`
	Status        string    `json:"status"`
	ActionSurface string    `json:"action_surface"`
	Created       time.Time `json:"created"`
	Body          string    `json:"body,omitempty"`
}
