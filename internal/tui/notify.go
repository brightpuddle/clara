package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type DynamicRegistration struct {
	Name       string `json:"name"`
	Token      string `json:"token"`
	SocketPath string `json:"socket_path"`
}

type NotificationAction struct {
	ID    string
	Title string
}

type Notification struct {
	ID             string
	SourceTool     string
	Title          string
	Subtitle       string
	Body           string
	Actions        []NotificationAction
	CreatedAt      time.Time
	RequiresAction bool
	WaitForReply   bool
	Status         string
	ResponseAction string
}

type NotificationEvent struct {
	Notification Notification
}

type NotificationResponse struct {
	NotificationID string         `json:"notification_id"`
	ActionID       string         `json:"action_id"`
	Status         string         `json:"status"`
	RespondedAt    string         `json:"responded_at,omitempty"`
	TimedOutAt     string         `json:"timed_out_at,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

type NotificationService struct {
	client *IPCClient
	events chan NotificationEvent

	mu         sync.Mutex
	pending    map[string]chan NotificationResponse
	cancel     context.CancelFunc
	conn       io.Closer
	registered bool
	serverName string
}

func NewNotificationService(client *IPCClient) *NotificationService {
	return &NotificationService{
		client:     client,
		events:     make(chan NotificationEvent, 32),
		pending:    make(map[string]chan NotificationResponse),
		serverName: "tui",
	}
}

func (s *NotificationService) Events() <-chan NotificationEvent {
	return s.events
}

func (s *NotificationService) Start(ctx context.Context) error {
	if !s.client.IsRunning() {
		return nil
	}
	reg, err := s.client.RegisterDynamicMCP(ctx, s.serverName)
	if err != nil {
		return err
	}
	conn, err := net.Dial("unix", reg.SocketPath)
	if err != nil {
		return errors.Wrap(err, "dial dynamic MCP socket")
	}
	if err := json.NewEncoder(conn).Encode(map[string]string{"token": reg.Token}); err != nil {
		_ = conn.Close()
		return errors.Wrap(err, "send dynamic MCP token")
	}
	var ack struct {
		Message string `json:"message,omitempty"`
		Error   string `json:"error,omitempty"`
	}
	if err := json.NewDecoder(conn).Decode(&ack); err != nil {
		_ = conn.Close()
		return errors.Wrap(err, "read dynamic MCP attach response")
	}
	if ack.Error != "" {
		_ = conn.Close()
		return fmt.Errorf("attach dynamic MCP peer: %s", ack.Error)
	}

	srv := server.NewMCPServer("clara-tui", "0.1.0")
	srv.AddTool(mcp.NewTool(
		"notify_send",
		mcp.WithDescription("Send a TUI notification without waiting for a response."),
		mcp.WithString("title", mcp.Required(), mcp.Description("Notification title.")),
		mcp.WithString("body", mcp.Required(), mcp.Description("Notification body.")),
		mcp.WithString("subtitle", mcp.Description("Optional subtitle.")),
		mcp.WithString(
			"source_tool",
			mcp.Description("Optional source tool name shown in the TUI."),
		),
	), s.handleNotifySend)
	srv.AddTool(mcp.NewTool(
		"notify_send_interactive",
		mcp.WithDescription("Send an interactive TUI notification with action buttons."),
		mcp.WithString("title", mcp.Required(), mcp.Description("Notification title.")),
		mcp.WithString("body", mcp.Required(), mcp.Description("Notification body.")),
		mcp.WithString("subtitle", mcp.Description("Optional subtitle.")),
		mcp.WithString(
			"source_tool",
			mcp.Description("Optional source tool name shown in the TUI."),
		),
		mcp.WithArray(
			"actions",
			mcp.Required(),
			mcp.Description("Action button list with id/title objects."),
		),
		mcp.WithBoolean(
			"wait_for_response",
			mcp.Description("When true, wait for the user to choose an action."),
		),
		mcp.WithNumber(
			"timeout_seconds",
			mcp.Description("Timeout for wait_for_response. Defaults to 60."),
		),
		mcp.WithString(
			"notification_id",
			mcp.Description("Optional stable notification identifier."),
		),
	), s.handleNotifyInteractive)

	stdio := server.NewStdioServer(srv)
	stdio.SetErrorLogger(log.New(io.Discard, "", 0))
	serveCtx, cancel := context.WithCancel(ctx)

	s.mu.Lock()
	s.cancel = cancel
	s.conn = conn
	s.registered = true
	s.mu.Unlock()

	go func() {
		_ = stdio.Listen(serveCtx, conn, conn)
	}()
	return nil
}

func (s *NotificationService) Close() {
	s.mu.Lock()
	cancel := s.cancel
	conn := s.conn
	registered := s.registered
	s.cancel = nil
	s.conn = nil
	s.registered = false
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if conn != nil {
		_ = conn.Close()
	}
	if registered {
		ctx, cancelCtx := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancelCtx()
		_ = s.client.UnregisterDynamicMCP(ctx, s.serverName)
	}
}

func (s *NotificationService) Respond(notificationID, actionID string) bool {
	s.mu.Lock()
	ch, ok := s.pending[notificationID]
	if ok {
		delete(s.pending, notificationID)
	}
	s.mu.Unlock()
	if !ok {
		return false
	}
	ch <- NotificationResponse{
		NotificationID: notificationID,
		ActionID:       actionID,
		Status:         "responded",
		RespondedAt:    time.Now().Format(time.RFC3339),
	}
	close(ch)
	return true
}

func (s *NotificationService) handleNotifySend(
	ctx context.Context,
	request mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args, err := toolArgs(request.Params.Arguments)
	if err != nil {
		return nil, err
	}
	n, err := buildNotification(args, false)
	if err != nil {
		return nil, err
	}
	s.events <- NotificationEvent{Notification: n}
	return mcp.NewToolResultStructured(map[string]any{
		"notification_id": n.ID,
		"status":          "sent",
		"delivered_at":    n.CreatedAt.Format(time.RFC3339),
	}, "notification sent"), nil
}

func (s *NotificationService) handleNotifyInteractive(
	ctx context.Context,
	request mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	args, err := toolArgs(request.Params.Arguments)
	if err != nil {
		return nil, err
	}
	n, err := buildNotification(args, true)
	if err != nil {
		return nil, err
	}
	waitForResponse, _ := optionalBool(args, "wait_for_response")
	timeout := optionalDuration(args, "timeout_seconds", 60*time.Second)
	n.WaitForReply = waitForResponse
	s.events <- NotificationEvent{Notification: n}

	if !waitForResponse {
		return mcp.NewToolResultStructured(map[string]any{
			"notification_id": n.ID,
			"status":          "sent",
			"delivered_at":    n.CreatedAt.Format(time.RFC3339),
		}, "interactive notification sent"), nil
	}

	respCh := make(chan NotificationResponse, 1)
	s.mu.Lock()
	s.pending[n.ID] = respCh
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, n.ID)
		s.mu.Unlock()
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-respCh:
		return mcp.NewToolResultStructured(map[string]any{
			"notification_id": resp.NotificationID,
			"action_id":       resp.ActionID,
			"status":          resp.Status,
			"responded_at":    resp.RespondedAt,
		}, "interactive notification responded"), nil
	case <-time.After(timeout):
		return mcp.NewToolResultStructured(map[string]any{
			"notification_id": n.ID,
			"status":          "timed_out",
			"timed_out_at":    time.Now().Format(time.RFC3339),
		}, "interactive notification timed out"), nil
	}
}

func buildNotification(args map[string]any, interactive bool) (Notification, error) {
	title, err := requiredString(args, "title")
	if err != nil {
		return Notification{}, err
	}
	body, err := requiredString(args, "body")
	if err != nil {
		return Notification{}, err
	}
	id, _ := optionalString(args, "notification_id")
	if id == "" {
		id = fmt.Sprintf("tui-%d", time.Now().UnixNano())
	}
	sourceTool, _ := optionalString(args, "source_tool")
	if sourceTool == "" {
		if interactive {
			sourceTool = "tui.notify_send_interactive"
		} else {
			sourceTool = "tui.notify_send"
		}
	}
	n := Notification{
		ID:             id,
		SourceTool:     sourceTool,
		Title:          title,
		Subtitle:       optionalStringDefault(args, "subtitle"),
		Body:           body,
		CreatedAt:      time.Now(),
		RequiresAction: interactive,
		Status:         "sent",
	}
	if interactive {
		actions, err := parseActions(args)
		if err != nil {
			return Notification{}, err
		}
		n.Actions = actions
	}
	return n, nil
}

func parseActions(args map[string]any) ([]NotificationAction, error) {
	raw, ok := args["actions"]
	if !ok {
		return nil, fmt.Errorf("actions array is required for interactive notifications")
	}
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil, fmt.Errorf("actions array is required for interactive notifications")
	}
	actions := make([]NotificationAction, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("interactive action entries must be objects")
		}
		id, err := requiredString(entry, "id")
		if err != nil {
			return nil, err
		}
		title, err := requiredString(entry, "title")
		if err != nil {
			return nil, err
		}
		actions = append(actions, NotificationAction{ID: id, Title: title})
	}
	return actions, nil
}

func toolArgs(raw any) (map[string]any, error) {
	args, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("tool arguments must be an object")
	}
	return args, nil
}

func requiredString(args map[string]any, key string) (string, error) {
	value, ok := args[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func optionalString(args map[string]any, key string) (string, bool) {
	value, ok := args[key].(string)
	return value, ok
}

func optionalStringDefault(args map[string]any, key string) string {
	value, _ := optionalString(args, key)
	return value
}

func optionalBool(args map[string]any, key string) (bool, bool) {
	value, ok := args[key].(bool)
	return value, ok
}

func optionalDuration(args map[string]any, key string, fallback time.Duration) time.Duration {
	value, ok := args[key]
	if !ok {
		return fallback
	}
	switch v := value.(type) {
	case float64:
		return time.Duration(v * float64(time.Second))
	case int:
		return time.Duration(v) * time.Second
	case int64:
		return time.Duration(v) * time.Second
	default:
		return fallback
	}
}
