package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
)

const description = "Webex integration: read and reply to messages (user account) and send bot notifications via the eve relay."

// Webex implements contract.Integration and contract.EventStreamer.
type Webex struct {
	cfg    Config
	client *http.Client
}

func newWebex() *Webex {
	return &Webex{
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (w *Webex) Configure(raw []byte) error {
	cfg, err := parseConfig(raw)
	if err != nil {
		return err
	}
	w.cfg = cfg
	return nil
}

func (w *Webex) Description() (string, error) { return description, nil }

func (w *Webex) Tools() ([]byte, error) {
	tools := []mcp.Tool{
		mcp.NewTool(
			"rooms.list",
			mcp.WithDescription(
				"List the user's Webex spaces and direct conversations, sorted by most recent activity.",
			),
			mcp.WithNumber("max", mcp.Description("Maximum number of rooms to return (default 50).")),
		),
		mcp.NewTool(
			"messages.list",
			mcp.WithDescription("List messages in a Webex room (newest first) using the user account."),
			mcp.WithString("room_id", mcp.Required(), mcp.Description("Webex room/space ID.")),
			mcp.WithNumber("max", mcp.Description("Maximum number of messages to return (default 50).")),
		),
		mcp.NewTool(
			"message.get",
			mcp.WithDescription("Retrieve a single Webex message by ID (includes full text)."),
			mcp.WithString("message_id", mcp.Required(), mcp.Description("Webex message ID.")),
		),
		mcp.NewTool(
			"message.reply",
			mcp.WithDescription("Send a reply to a Webex room using the user account."),
			mcp.WithString("room_id", mcp.Required(), mcp.Description("Webex room/space ID to post into.")),
			mcp.WithString("text", mcp.Required(), mcp.Description("Plain-text message body.")),
		),
		mcp.NewTool(
			"notification.send",
			mcp.WithDescription("Send a notification message to a Webex room via the bot account."),
			mcp.WithString("room_id", mcp.Required(), mcp.Description("Webex room/space ID to post into.")),
			mcp.WithString("text", mcp.Required(), mcp.Description("Notification message text.")),
		),
		mcp.NewTool(
			"approval.request",
			mcp.WithDescription(
				"Post an approval card with Approve/Reject buttons and block until decided. "+
					"Returns \"approved\", \"rejected\", or \"timeout\".",
			),
			mcp.WithString(
				"room_id",
				mcp.Required(),
				mcp.Description("Target Webex room ID."),
			),
			mcp.WithString("title", mcp.Required(), mcp.Description("Short title for the approval card.")),
			mcp.WithString("description", mcp.Description("Detail shown in the card body.")),
			mcp.WithNumber("timeout_s", mcp.Description("Seconds to wait for decision (default 300).")),
		),
		mcp.NewTool(
			"message_created",
			mcp.WithDescription(
				"Event source: fired when a new Webex message is received (via webhook callback). "+
					"Use as a trigger: clara.on(webex.message_created). "+
					"Event data: {message_id, room_id, room_type, person_email, person_id, text, created}.",
			),
		),
	}
	return json.Marshal(tools)
}

func (w *Webex) CallTool(name string, args []byte) ([]byte, error) {
	switch name {
	case "rooms.list":
		return w.callRoomsList(args)
	case "messages.list":
		return w.callMessagesList(args)
	case "message.get":
		return w.callMessageGet(args)
	case "message.reply":
		return w.callMessageReply(args)
	case "notification.send":
		return w.callNotificationSend(args)
	case "approval.request":
		return w.callApprovalRequest(args)
	case "message_created":
		return json.Marshal(map[string]string{
			"error": "message_created is an event source, not a callable tool",
		})
	default:
		return nil, errors.Newf("webex: unknown tool %q", name)
	}
}

// --- tool implementations ---

type roomsListArgs struct {
	Max float64 `json:"max"`
}

func (w *Webex) callRoomsList(args []byte) ([]byte, error) {
	var a roomsListArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, errors.Wrap(err, "webex rooms.list: unmarshal")
	}
	max := int(a.Max)
	if max <= 0 {
		max = 50
	}
	return w.get("/api/webex/rooms", map[string]string{"max": strconv.Itoa(max)})
}

type messagesListArgs struct {
	RoomID string  `json:"room_id"`
	Max    float64 `json:"max"`
}

func (w *Webex) callMessagesList(args []byte) ([]byte, error) {
	var a messagesListArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, errors.Wrap(err, "webex messages.list: unmarshal")
	}
	if a.RoomID == "" {
		return nil, errors.New("webex messages.list: room_id is required")
	}
	max := int(a.Max)
	if max <= 0 {
		max = 50
	}
	return w.get("/api/webex/messages", map[string]string{
		"room_id": a.RoomID,
		"max":     strconv.Itoa(max),
	})
}

type messageGetArgs struct {
	MessageID string `json:"message_id"`
}

func (w *Webex) callMessageGet(args []byte) ([]byte, error) {
	var a messageGetArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, errors.Wrap(err, "webex message.get: unmarshal")
	}
	if a.MessageID == "" {
		return nil, errors.New("webex message.get: message_id is required")
	}
	return w.getPath("/api/webex/messages/" + a.MessageID)
}

type messageReplyArgs struct {
	RoomID string `json:"room_id"`
	Text   string `json:"text"`
}

func (w *Webex) callMessageReply(args []byte) ([]byte, error) {
	var a messageReplyArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, errors.Wrap(err, "webex message.reply: unmarshal")
	}
	if a.RoomID == "" || a.Text == "" {
		return nil, errors.New("webex message.reply: room_id and text are required")
	}
	return w.post("/api/webex/message", map[string]any{"room_id": a.RoomID, "text": a.Text})
}

type notificationSendArgs struct {
	RoomID string `json:"room_id"`
	Text   string `json:"text"`
}

func (w *Webex) callNotificationSend(args []byte) ([]byte, error) {
	var a notificationSendArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, errors.Wrap(err, "webex notification.send: unmarshal")
	}
	if a.RoomID == "" || a.Text == "" {
		return nil, errors.New("webex notification.send: room_id and text are required")
	}
	return w.post("/api/webex/notification", map[string]any{"room_id": a.RoomID, "text": a.Text})
}

type approvalRequestArgs struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	RoomID      string  `json:"room_id"`
	TimeoutS    float64 `json:"timeout_s"`
}

func (w *Webex) callApprovalRequest(args []byte) ([]byte, error) {
	var a approvalRequestArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, errors.Wrap(err, "webex approval.request: unmarshal")
	}
	if a.RoomID == "" {
		return nil, errors.New("webex approval.request: room_id is required")
	}
	timeoutSec := int(a.TimeoutS)
	if timeoutSec <= 0 {
		timeoutSec = 300
	}
	requestID := uuid.New().String()
	// Post the approval message to Webex via the relay.
	_, err := w.post("/api/webex/approval", map[string]any{
		"request_id":  requestID,
		"machine":     w.cfg.Machine,
		"room_id":     a.RoomID,
		"title":       a.Title,
		"description": a.Description,
	})
	if err != nil {
		return nil, errors.Wrap(err, "webex approval.request: post")
	}
	// Long-poll for the decision.
	decision, err := w.waitDecision(requestID, timeoutSec)
	if err != nil {
		return nil, errors.Wrap(err, "webex approval.request: wait")
	}
	return json.Marshal(map[string]string{"decision": decision})
}

// waitDecision long-polls GET /api/webex/approval/{id}?timeout=N.
func (w *Webex) waitDecision(requestID string, timeoutSec int) (string, error) {
	url := fmt.Sprintf("%s/api/webex/approval/%s?timeout=%d", w.cfg.EveURL, requestID, timeoutSec)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", errors.Wrap(err, "build request")
	}
	req.Header.Set("Authorization", "Bearer "+w.cfg.Secret)

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

// --- HTTP helpers ---

func (w *Webex) get(path string, params map[string]string) ([]byte, error) {
	url := w.cfg.EveURL + path
	if len(params) > 0 {
		sep := "?"
		for k, v := range params {
			url += sep + k + "=" + v
			sep = "&"
		}
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.Wrap(err, "build request")
	}
	req.Header.Set("Authorization", "Bearer "+w.cfg.Secret)
	return w.do(req, path)
}

func (w *Webex) getPath(path string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, w.cfg.EveURL+path, nil)
	if err != nil {
		return nil, errors.Wrap(err, "build request")
	}
	req.Header.Set("Authorization", "Bearer "+w.cfg.Secret)
	return w.do(req, path)
}

func (w *Webex) post(path string, payload map[string]any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.Wrap(err, "marshal payload")
	}
	req, err := http.NewRequest(http.MethodPost, w.cfg.EveURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, errors.Wrap(err, "build request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+w.cfg.Secret)
	return w.do(req, path)
}

func (w *Webex) do(req *http.Request, path string) ([]byte, error) {
	resp, err := w.client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "http request")
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, errors.Newf("eve relay %s: status %d: %s", path, resp.StatusCode, respBody)
	}
	return respBody, nil
}
