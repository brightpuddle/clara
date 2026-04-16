package webex

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	cards "github.com/DanielTitkov/go-adaptive-cards"
	"github.com/WebexCommunity/webex-go-sdk/v2"
	"github.com/WebexCommunity/webex-go-sdk/v2/attachmentactions"
	"github.com/WebexCommunity/webex-go-sdk/v2/memberships"
	"github.com/WebexCommunity/webex-go-sdk/v2/mercury"
	"github.com/WebexCommunity/webex-go-sdk/v2/messages"
	"github.com/WebexCommunity/webex-go-sdk/v2/rooms"
	"github.com/WebexCommunity/webex-go-sdk/v2/webexsdk"
	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
)

const (
	Description = "Built-in Webex MCP server for messaging and space management with Adaptive Card support."
)

type Service struct {
	accessToken string
	client      *webex.WebexClient
	mc          *mercury.Client
	log         zerolog.Logger

	mercuryEnabled   bool
	pendingActions   map[string]chan map[string]any
	pendingActionsMu sync.Mutex

	stopMercury func()
}

func New(accessToken string, httpClient *http.Client, log zerolog.Logger) *Service {
	cfg := &webexsdk.Config{
		HttpClient: httpClient,
	}
	client, _ := webex.NewClient(accessToken, cfg)

	return &Service{
		accessToken:    accessToken,
		client:         client,
		log:            log.With().Str("component", "mcp_webex").Logger(),
		pendingActions: make(map[string]chan map[string]any),
	}
}

func (s *Service) Start(ctx context.Context) error {
	s.mc = s.client.Mercury()

	// Handle attachmentActions (Adaptive Cards)
	s.mc.On("attachmentAction:created", func(event *mercury.Event) {
		s.log.Debug().Str("event_id", event.ID).Msg("webex attachmentAction created event")

		// Extract action data from mercury event
		data := event.Data
		if data == nil {
			return
		}

		actionID, _ := data["id"].(string)
		if actionID == "" {
			return
		}

		// Fetch full details
		action, err := s.client.AttachmentActions().Get(actionID)
		if err != nil {
			s.log.Error().Err(err).Str("action_id", actionID).Msg("failed to fetch attachment action details")
			return
		}

		s.handleAttachmentAction(action)
	})

	err := s.mc.Connect()
	if err != nil {
		s.log.Warn().Err(err).Msg("failed to connect to Webex Mercury (WebSocket). Real-time features and blocking Adaptive Card waits will be disabled unless webhooks are used.")
		s.mercuryEnabled = false
		return nil // Non-fatal
	}

	s.log.Info().Msg("Webex Mercury listener connected")
	s.mercuryEnabled = true

	s.stopMercury = func() {
		_ = s.mc.Disconnect()
	}

	return nil
}

func (s *Service) Stop() {
	if s.stopMercury != nil {
		s.stopMercury()
	}
}

// HandleWebhook allows external servers to push events into Clara.
// This is the fallback for when Mercury (WebSocket) is unauthorized.
func (s *Service) HandleWebhook(action *attachmentactions.AttachmentAction) {
	s.log.Debug().Str("action_id", action.ID).Msg("received attachment action via webhook")
	s.handleAttachmentAction(action)
}

func (s *Service) handleAttachmentAction(action *attachmentactions.AttachmentAction) {
	s.pendingActionsMu.Lock()
	defer s.pendingActionsMu.Unlock()

	var correlationID string
	if action.Inputs != nil {
		if cid, ok := action.Inputs["clara_correlation_id"].(string); ok {
			correlationID = cid
		}
	}

	if correlationID == "" {
		s.log.Debug().Msg("attachment action received without clara_correlation_id")
		return
	}

	if ch, ok := s.pendingActions[correlationID]; ok {
		ch <- action.Inputs
		close(ch)
		delete(s.pendingActions, correlationID)
		s.log.Debug().Str("correlation_id", correlationID).Msg("routed attachment action to pending waiter")
	} else {
		s.log.Debug().Str("correlation_id", correlationID).Msg("received attachment action for unknown/expired correlation id")
	}
}

func (s *Service) NewServer() *server.MCPServer {
	mcpServer := server.NewMCPServer(
		"clara-webex",
		"0.2.1",
		server.WithToolCapabilities(true),
		server.WithInstructions(Description),
	)

	mcpServer.AddTool(mcp.NewTool("list_spaces",
		mcp.WithDescription("List Webex spaces (rooms) the user is a member of."),
		mcp.WithNumber("max", mcp.Description("Maximum number of spaces to return.")),
		mcp.WithString("type", mcp.Description("Filter by space type: 'direct' or 'group'.")),
		mcp.WithString("sortBy", mcp.Description("Sort results by: 'id', 'lastactivity', or 'created'.")),
	), s.handleListSpaces)

	mcpServer.AddTool(mcp.NewTool("get_messages",
		mcp.WithDescription("List messages in a Webex space."),
		mcp.WithString("room_id", mcp.Required(), mcp.Description("The ID of the space.")),
		mcp.WithNumber("max", mcp.Description("Maximum number of messages to return.")),
		mcp.WithString("before", mcp.Description("List messages sent before a specific date/time (ISO-8601).")),
	), s.handleGetMessages)

	mcpServer.AddTool(mcp.NewTool("send_message",
		mcp.WithDescription("Send a message to a Webex space or person."),
		mcp.WithString("room_id", mcp.Description("The ID of the space to post to.")),
		mcp.WithString("to_person_email", mcp.Description("The email of the person to send a direct message to.")),
		mcp.WithString("to_person_id", mcp.Description("The ID of the person to send a direct message to.")),
		mcp.WithString("text", mcp.Description("The message in plain text.")),
		mcp.WithString("markdown", mcp.Description("The message in Markdown format.")),
	), s.handleSendMessage)

	mcpServer.AddTool(mcp.NewTool("hide_space",
		mcp.WithDescription("Hide a Webex space from the space list."),
		mcp.WithString("room_id", mcp.Required(), mcp.Description("The ID of the space to hide.")),
	), s.handleHideSpace)

	mcpServer.AddTool(mcp.NewTool("unhide_space",
		mcp.WithDescription("Unhide a Webex space."),
		mcp.WithString("room_id", mcp.Required(), mcp.Description("The ID of the space to unhide.")),
	), s.handleUnhideSpace)

	mcpServer.AddTool(mcp.NewTool("search_messages",
		mcp.WithDescription("Search for messages containing a keyword across recent spaces."),
		mcp.WithString("query", mcp.Required(), mcp.Description("The keyword to search for.")),
		mcp.WithNumber("space_limit", mcp.Description("Maximum number of recent spaces to search (default: 10).")),
		mcp.WithNumber("message_limit", mcp.Description("Maximum number of recent messages to fetch per space (default: 20).")),
	), s.handleSearchMessages)

	mcpServer.AddTool(mcp.NewTool("send_adaptive_card",
		mcp.WithDescription("Send an Adaptive Card to a Webex space and wait for a response."),
		mcp.WithString("room_id", mcp.Required(), mcp.Description("The ID of the space to post to.")),
		mcp.WithString("task_id", mcp.Required(), mcp.Description("Unique task ID to correlate the response.")),
		mcp.WithString("title", mcp.Description("The title of the card.")),
		mcp.WithString("body", mcp.Description("The body text of the card.")),
		mcp.WithBoolean("wait", mcp.Description("Whether to wait for a response (default: true).")),
	), s.handleSendAdaptiveCard)

	return mcpServer
}

func (s *Service) handleListSpaces(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	opts := &rooms.ListOptions{}
	if max, ok := req.GetArguments()["max"].(float64); ok {
		opts.Max = int(max)
	}
	if t, ok := req.GetArguments()["type"].(string); ok {
		opts.Type = t
	}
	if sortBy, ok := req.GetArguments()["sortBy"].(string); ok {
		opts.SortBy = sortBy
	} else {
		opts.SortBy = "lastactivity"
	}

	roomsPage, err := s.client.Rooms().List(opts)
	if err != nil {
		if webexsdk.IsRateLimited(err) {
			return toolErrorResult("list_spaces", errors.New("Webex API rate limited")), nil
		}
		if webexsdk.IsAuthError(err) {
			return toolErrorResult("list_spaces", errors.New("Webex authentication failed")), nil
		}
		return toolErrorResult("list_spaces", err), nil
	}

	return structuredResult(roomsPage.Items)
}

func (s *Service) handleGetMessages(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	roomID, ok := req.GetArguments()["room_id"].(string)
	if !ok || roomID == "" {
		return mcp.NewToolResultError("room_id is required"), nil
	}

	opts := &messages.ListOptions{RoomID: roomID}
	if max, ok := req.GetArguments()["max"].(float64); ok {
		opts.Max = int(max)
	}
	if before, ok := req.GetArguments()["before"].(string); ok {
		opts.Before = before
	}

	messagesPage, err := s.client.Messages().List(opts)
	if err != nil {
		return toolErrorResult("get_messages", err), nil
	}

	return structuredResult(messagesPage.Items)
}

func (s *Service) handleSendMessage(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	msgRequest := &messages.Message{}
	if roomID, ok := req.GetArguments()["room_id"].(string); ok {
		msgRequest.RoomID = roomID
	}
	if email, ok := req.GetArguments()["to_person_email"].(string); ok {
		msgRequest.ToPersonEmail = email
	}
	if personID, ok := req.GetArguments()["to_person_id"].(string); ok {
		msgRequest.ToPersonID = personID
	}
	if text, ok := req.GetArguments()["text"].(string); ok {
		msgRequest.Text = text
	}
	if markdown, ok := req.GetArguments()["markdown"].(string); ok {
		msgRequest.Markdown = markdown
	}

	msg, err := s.client.Messages().Create(msgRequest)
	if err != nil {
		return toolErrorResult("send_message", err), nil
	}
	return structuredResult(msg)
}

func (s *Service) handleHideSpace(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.setRoomHidden(ctx, req, true)
}

func (s *Service) handleUnhideSpace(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.setRoomHidden(ctx, req, false)
}

func (s *Service) setRoomHidden(ctx context.Context, req mcp.CallToolRequest, hidden bool) (*mcp.CallToolResult, error) {
	roomID, ok := req.GetArguments()["room_id"].(string)
	if !ok || roomID == "" {
		return mcp.NewToolResultError("room_id is required"), nil
	}

	// First get the membership ID for "me" in this room
	membershipsPage, err := s.client.Memberships().List(&memberships.ListOptions{
		RoomID:   roomID,
		PersonID: "me",
	})
	if err != nil {
		return toolErrorResult("setRoomHidden", err), nil
	}
	if len(membershipsPage.Items) == 0 {
		return mcp.NewToolResultError("membership not found for room"), nil
	}

	m := membershipsPage.Items[0]
	m.IsRoomHidden = hidden

	updated, err := s.client.Memberships().Update(m.ID, &m)
	if err != nil {
		return toolErrorResult("setRoomHidden", err), nil
	}
	return structuredResult(updated)
}

type searchMatch struct {
	SpaceName string           `json:"space_name"`
	Message   messages.Message `json:"message"`
}

func (s *Service) handleSearchMessages(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query, ok := req.GetArguments()["query"].(string)
	if !ok || query == "" {
		return mcp.NewToolResultError("query is required"), nil
	}

	spaceLimit := 10
	if limit, ok := req.GetArguments()["space_limit"].(float64); ok {
		spaceLimit = int(limit)
	}
	msgLimit := 20
	if limit, ok := req.GetArguments()["message_limit"].(float64); ok {
		msgLimit = int(limit)
	}

	roomsPage, err := s.client.Rooms().List(&rooms.ListOptions{
		Max:    spaceLimit,
		SortBy: "lastactivity",
	})
	if err != nil {
		return toolErrorResult("search_messages", err), nil
	}

	var matches []searchMatch
	var matchesMu sync.Mutex
	var wg sync.WaitGroup

	queryLower := strings.ToLower(query)

	for _, room := range roomsPage.Items {
		wg.Add(1)
		go func(roomID, roomTitle string) {
			defer wg.Done()
			messagesPage, err := s.client.Messages().List(&messages.ListOptions{
				RoomID: roomID,
				Max:    msgLimit,
			})
			if err != nil {
				s.log.Warn().Err(err).Str("room_id", roomID).Msg("failed to fetch messages for search")
				return
			}

			for _, msg := range messagesPage.Items {
				if strings.Contains(strings.ToLower(msg.Text), queryLower) {
					matchesMu.Lock()
					matches = append(matches, searchMatch{
						SpaceName: roomTitle,
						Message:   msg,
					})
					matchesMu.Unlock()
				}
			}
		}(room.ID, room.Title)
	}

	wg.Wait()

	return structuredResult(matches)
}

func (s *Service) handleSendAdaptiveCard(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	roomID, _ := req.GetArguments()["room_id"].(string)
	title, _ := req.GetArguments()["title"].(string)
	body, _ := req.GetArguments()["body"].(string)
	taskID, _ := req.GetArguments()["task_id"].(string)
	wait, ok := req.GetArguments()["wait"].(bool)
	if !ok {
		wait = true
	}

	if roomID == "" || taskID == "" {
		return mcp.NewToolResultError("room_id and task_id are required"), nil
	}

	if title == "" {
		title = "Clara Task Action Required"
	}
	if body == "" {
		body = "Please provide your input for the following task."
	}

	// Build Adaptive Card using go-adaptive-cards
	card := cards.New([]cards.Node{
		&cards.TextBlock{
			Text:   title,
			Size:   "Large",
			Weight: "Bolder",
		},
		&cards.TextBlock{
			Text: body,
			Wrap: cards.TruePtr(),
		},
		&cards.InputText{
			ID:          "user_input",
			Placeholder: "Type your response here...",
		},
	}, []cards.Node{
		&cards.ActionSubmit{
			Title: "Submit",
			Data: map[string]any{
				"clara_correlation_id": taskID,
			},
		},
	}).WithVersion(cards.Version12)

	// Post the card as an attachment
	msgRequest := &messages.Message{
		RoomID: roomID,
		Attachments: []messages.Attachment{
			{
				ContentType: "application/vnd.microsoft.card.adaptive",
				Content:     card,
			},
		},
	}

	_, err := s.client.Messages().Create(msgRequest)
	if err != nil {
		return toolErrorResult("send_adaptive_card", err), nil
	}

	if !wait {
		return mcp.NewToolResultText("Adaptive card sent"), nil
	}

	// Create a channel to wait for the response
	respChan := make(chan map[string]any, 1)
	s.pendingActionsMu.Lock()
	s.pendingActions[taskID] = respChan
	s.pendingActionsMu.Unlock()

	select {
	case <-ctx.Done():
		s.pendingActionsMu.Lock()
		delete(s.pendingActions, taskID)
		s.pendingActionsMu.Unlock()
		return mcp.NewToolResultError("timeout waiting for adaptive card response"), nil
	case response := <-respChan:
		return structuredResult(response)
	}
}

func structuredResult(value any) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultStructuredOnly(value), nil
}

func toolErrorResult(tool string, err error) *mcp.CallToolResult {
	return mcp.NewToolResultError(fmt.Sprintf("%s: %v", tool, err))
}
