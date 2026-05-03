package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
)

const description = "Discord integration: send messages, notifications, and approval requests via the eve relay."

func levelColor(level string) int {
	switch level {
	case "warn":
		return 0xFFA500
	case "danger":
		return 0xFF0000
	default:
		return 0x5865F2
	}
}

// Discord implements contract.Integration and contract.EventStreamer.
type Discord struct {
	cfg    Config
	client *http.Client
}

func newDiscord() *Discord {
	return &Discord{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (d *Discord) Configure(raw []byte) error {
	cfg, err := parseConfig(raw)
	if err != nil {
		return err
	}
	d.cfg = cfg
	return nil
}

func (d *Discord) Description() (string, error) { return description, nil }

func (d *Discord) Tools() ([]byte, error) {
	tools := []mcp.Tool{
		mcp.NewTool(
			"message.send",
			mcp.WithDescription("Send a plain or embedded message to a Discord channel."),
			mcp.WithString("channel_id", mcp.Required(), mcp.Description("Target Discord channel ID.")),
			mcp.WithString("content", mcp.Description("Plain text message content.")),
		),
		mcp.NewTool(
			"notification.send",
			mcp.WithDescription("Send a titled embed notification to a Discord channel."),
			mcp.WithString(
				"channel_id",
				mcp.Required(),
				mcp.Description("Target Discord channel ID."),
			),
			mcp.WithString("title", mcp.Required(), mcp.Description("Notification title.")),
			mcp.WithString("body", mcp.Required(), mcp.Description("Notification body text.")),
			mcp.WithString("level", mcp.Description("Severity: info (default), warn, or danger.")),
		),
		mcp.NewTool(
			"approval.request",
			mcp.WithDescription(
				"Post an approval embed with Approve/Reject buttons and block until decided. "+
					"Returns \"approved\", \"rejected\", or \"timeout\".",
			),
			mcp.WithString(
				"channel_id",
				mcp.Required(),
				mcp.Description("Target Discord channel ID."),
			),
			mcp.WithString("title", mcp.Required(), mcp.Description("Short title for the approval card.")),
			mcp.WithString("description", mcp.Description("Detail shown in the embed body.")),
			mcp.WithNumber("timeout_s", mcp.Description("Seconds to wait for decision (default 300).")),
		),
		mcp.NewTool(
			"message_created",
			mcp.WithDescription(
				"Event source: fired when a message is posted in any Discord channel the bot can read. "+
					"Use as a trigger: clara.on(discord.message_created). "+
					"Event data: {channel_id, message_id, user, content}. "+
					"Filter by channel_id inside the handler.",
			),
		),
	}
	return json.Marshal(tools)
}

func (d *Discord) CallTool(name string, args []byte) ([]byte, error) {
	switch name {
	case "message.send":
		return d.callMessageSend(args)
	case "notification.send":
		return d.callNotificationSend(args)
	case "approval.request":
		return d.callApprovalRequest(args)
	case "message_created":
		// Event source — not directly callable.
		return json.Marshal(map[string]string{"error": "message_created is an event source, not a callable tool"})
	default:
		return nil, errors.Newf("discord: unknown tool %q", name)
	}
}

// --- Tool implementations ---

type messageSendArgs struct {
	ChannelID string `json:"channel_id"`
	Content   string `json:"content"`
}

func (d *Discord) callMessageSend(args []byte) ([]byte, error) {
	var a messageSendArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, errors.Wrap(err, "discord message.send: unmarshal")
	}
	resp, err := d.post("/api/discord/message", map[string]any{
		"channel_id": a.ChannelID,
		"content":    a.Content,
	})
	if err != nil {
		return nil, errors.Wrap(err, "discord message.send")
	}
	return json.Marshal(map[string]string{"message_id": resp["message_id"]})
}

type notificationSendArgs struct {
	ChannelID string `json:"channel_id"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Level     string `json:"level"`
}

func (d *Discord) callNotificationSend(args []byte) ([]byte, error) {
	var a notificationSendArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, errors.Wrap(err, "discord notification.send: unmarshal")
	}
	if a.ChannelID == "" {
		return nil, errors.New("discord notification.send: channel_id is required")
	}
	if a.Level == "" {
		a.Level = "info"
	}
	resp, err := d.post("/api/discord/message", map[string]any{
		"channel_id": a.ChannelID,
		"embed": map[string]any{
			"title":       a.Title,
			"description": a.Body,
			"color":       levelColor(a.Level),
		},
	})
	if err != nil {
		return nil, errors.Wrap(err, "discord notification.send")
	}
	return json.Marshal(map[string]string{"message_id": resp["message_id"]})
}

type approvalRequestArgs struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	ChannelID   string  `json:"channel_id"`
	TimeoutS    float64 `json:"timeout_s"`
}

func (d *Discord) callApprovalRequest(args []byte) ([]byte, error) {
	var a approvalRequestArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, errors.Wrap(err, "discord approval.request: unmarshal")
	}
	if a.ChannelID == "" {
		return nil, errors.New("discord approval.request: channel_id is required")
	}
	timeoutSec := int(a.TimeoutS)
	if timeoutSec <= 0 {
		timeoutSec = 300
	}
	requestID := uuid.New().String()
	// Post the approval message to Discord via the relay.
	_, err := d.post("/api/discord/approval", map[string]any{
		"request_id":  requestID,
		"machine":     d.cfg.Machine,
		"channel_id":  a.ChannelID,
		"title":       a.Title,
		"description": a.Description,
	})
	if err != nil {
		return nil, errors.Wrap(err, "discord approval.request: post")
	}
	// Long-poll for the decision.
	decision, err := d.waitDecision(requestID, timeoutSec)
	if err != nil {
		return nil, errors.Wrap(err, "discord approval.request: wait")
	}
	return json.Marshal(map[string]string{"decision": decision})
}

// waitDecision long-polls GET /api/discord/approval/{id}?timeout=N.
func (d *Discord) waitDecision(requestID string, timeoutSec int) (string, error) {
	url := fmt.Sprintf("%s/api/discord/approval/%s?timeout=%d", d.cfg.EveURL, requestID, timeoutSec)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", errors.Wrap(err, "build request")
	}
	req.Header.Set("Authorization", "Bearer "+d.cfg.Secret)

	// Client timeout must exceed the server-side wait.
	longClient := &http.Client{Timeout: time.Duration(timeoutSec+15) * time.Second}
	resp, err := longClient.Do(req)
	if err != nil {
		return "", errors.Wrap(err, "http request")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusRequestTimeout {
		return "timeout", nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", errors.Newf("approval poll: status %d: %s", resp.StatusCode, body)
	}
	var result struct {
		Decision string `json:"decision"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", errors.Wrap(err, "unmarshal decision")
	}
	return result.Decision, nil
}

func (d *Discord) post(path string, payload map[string]any) (map[string]string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.Wrap(err, "marshal payload")
	}
	req, err := http.NewRequest(http.MethodPost, d.cfg.EveURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, errors.Wrap(err, "build request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+d.cfg.Secret)

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "http request")
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, errors.Newf("eve relay %s: status %d: %s", path, resp.StatusCode, respBody)
	}
	var result map[string]string
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, errors.Wrap(err, "unmarshal response")
	}
	return result, nil
}
