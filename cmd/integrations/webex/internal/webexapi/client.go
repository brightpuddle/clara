package webexapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const apiBase = "https://webexapis.com/v1"

// Room represents a Webex space or direct conversation.
type Room struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Type     string `json:"type"` // "direct" or "group"
	IsLocked bool   `json:"isLocked"`
}

// Message represents a Webex message.
type Message struct {
	ID          string `json:"id"`
	RoomID      string `json:"roomId"`
	RoomType    string `json:"roomType"`
	Text        string `json:"text"`
	PersonEmail string `json:"personEmail"`
	PersonID    string `json:"personId"`
	Created     string `json:"created"`
}

// AttachmentAction represents a button click on an adaptive card.
type AttachmentAction struct {
	ID       string         `json:"id"`
	PersonID string         `json:"personId"`
	Inputs   map[string]any `json:"inputs"`
}

// Client wraps the Webex REST API for a single identity.
type Client struct {
	getToken func() (string, error)
	client   *http.Client
}

// NewBotClient creates a Client with a permanent, static bot token.
func NewBotClient(token string) *Client {
	return &Client{
		getToken: func() (string, error) { return token, nil },
		client:   &http.Client{Timeout: 15 * time.Second},
	}
}

// NewUserClient creates a Client backed by a TokenManager.
func NewUserClient(tm *TokenManager) *Client {
	return &Client{
		getToken: tm.AccessToken,
		client:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) ListRooms(max int) ([]Room, error) {
	if max <= 0 {
		max = 50
	}
	var result struct {
		Items []Room `json:"items"`
	}
	if err := c.get(fmt.Sprintf("/rooms?sortBy=lastactivity&max=%d", max), &result); err != nil {
		return nil, err
	}
	return result.Items, nil
}

func (c *Client) ListMessages(roomID string, max int) ([]Message, error) {
	if max <= 0 {
		max = 50
	}
	path := fmt.Sprintf("/messages?roomId=%s&max=%d", url.QueryEscape(roomID), max)
	var result struct {
		Items []Message `json:"items"`
	}
	if err := c.get(path, &result); err != nil {
		return nil, err
	}
	return result.Items, nil
}

func (c *Client) GetMessage(id string) (*Message, error) {
	var msg Message
	if err := c.get("/messages/"+url.PathEscape(id), &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func (c *Client) SendMessage(roomID, text string) (*Message, error) {
	var msg Message
	if err := c.post("/messages", map[string]any{"roomId": roomID, "text": text}, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// GetAttachmentAction fetches details of a button click.
func (c *Client) GetAttachmentAction(id string) (*AttachmentAction, error) {
	var action AttachmentAction
	if err := c.get("/attachment/actions/"+url.PathEscape(id), &action); err != nil {
		return nil, err
	}
	return &action, nil
}

// SendInteractive posts an adaptive card with multiple choice buttons and an optional text input.
func (c *Client) SendInteractive(roomID, machine, requestID, title, description string, options []string, allowText bool) (*Message, error) {
	if len(options) == 0 {
		options = []string{"Approve", "Reject"}
	}

	actions := []any{}
	for _, opt := range options {
		actions = append(actions, map[string]any{
			"type":  "Action.Submit",
			"title": opt,
			"data": map[string]string{
				"request_id": requestID,
				"decision":   opt,
			},
		})
	}

	if allowText {
		actions = append(actions, map[string]any{
			"type":  "Action.ShowCard",
			"title": "Feedback / Other...",
			"card": map[string]any{
				"type":    "AdaptiveCard",
				"version": "1.0",
				"body": []any{
					map[string]any{
						"type":        "Input.Text",
						"id":          "custom_text",
						"placeholder": "Enter your response...",
						"isMultiline": true,
					},
				},
				"actions": []any{
					map[string]any{
						"type":  "Action.Submit",
						"title": "Submit Feedback",
						"data": map[string]string{
							"request_id": requestID,
							"decision":   "custom",
						},
					},
				},
			},
		})
	}

	card := map[string]any{
		"type":    "AdaptiveCard",
		"version": "1.0",
		"body": []any{
			map[string]any{
				"type":   "TextBlock",
				"text":   title,
				"weight": "Bolder",
				"size":   "Medium",
			},
		},
		"actions": actions,
	}
	if description != "" {
		card["body"] = append(card["body"].([]any), map[string]any{
			"type": "TextBlock",
			"text": description,
			"wrap": true,
		})
	}

	payload := map[string]any{
		"roomId": roomID,
		"text":   fmt.Sprintf("Interactive Request: %s", title),
		"attachments": []any{
			map[string]any{
				"contentType": "application/vnd.microsoft.card.adaptive",
				"content":     card,
			},
		},
	}

	var msg Message
	if err := c.post("/messages", payload, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// --- helpers ---

func (c *Client) get(path string, out any) error {
	token, err := c.getToken()
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodGet, apiBase+path, nil)
	if err != nil {
		return fmt.Errorf("webex: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("webex: GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webex: GET %s: status %d: %s", path, resp.StatusCode, body)
	}
	return json.Unmarshal(body, out)
}

func (c *Client) post(path string, payload any, out any) error {
	token, err := c.getToken()
	if err != nil {
		return err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("webex: marshal payload: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, apiBase+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("webex: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("webex: POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webex: POST %s: status %d: %s", path, resp.StatusCode, body)
	}
	if out != nil {
		return json.Unmarshal(body, out)
	}
	return nil
}