package webex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
)

const (
	Description = "Built-in Webex MCP server for messaging and space management."
	BaseURL     = "https://webexapis.com/v1"
)

type Service struct {
	accessToken      string
	httpClient       *http.Client
	log              zerolog.Logger
	myPersonID       string
	membershipsCache map[string]map[string]any
	membershipsMu    sync.Mutex
}

func (s *Service) getMyPersonID(ctx context.Context) (string, error) {
	if s.myPersonID != "" {
		return s.myPersonID, nil
	}
	var result map[string]any
	if err := s.doRequest(ctx, "GET", "/people/me", nil, nil, &result); err != nil {
		return "", err
	}
	id, ok := result["id"].(string)
	if !ok || id == "" {
		return "", errors.New("could not find my person id")
	}
	s.myPersonID = id
	return id, nil
}

func New(accessToken string, log zerolog.Logger) *Service {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 100
	t.MaxConnsPerHost = 4
	t.MaxIdleConnsPerHost = 4

	return &Service{
		accessToken: accessToken,
		httpClient:  &http.Client{Timeout: 30 * time.Second, Transport: t},
		log:         log.With().Str("component", "mcp_webex").Logger(),
	}
}

func (s *Service) NewServer() *server.MCPServer {
	mcpServer := server.NewMCPServer(
		"clara-webex",
		"0.1.0",
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

	mcpServer.AddTool(mcp.NewTool("get_direct_messages",
		mcp.WithDescription("List 1:1 messages with a specific person."),
		mcp.WithString("person_id", mcp.Description("The ID of the person.")),
		mcp.WithString("person_email", mcp.Description("The email of the person.")),
	), s.handleGetDirectMessages)

	mcpServer.AddTool(mcp.NewTool("search_messages",
		mcp.WithDescription("Search for messages containing a keyword across recent spaces. Since Webex has no global search API, this tool fetches recent messages from the most active spaces and filters them locally."),
		mcp.WithString("query", mcp.Required(), mcp.Description("The keyword to search for.")),
		mcp.WithNumber("space_limit", mcp.Description("Maximum number of recent spaces to search (default: 10).")),
		mcp.WithNumber("message_limit", mcp.Description("Maximum number of recent messages to fetch per space (default: 20).")),
	), s.handleSearchMessages)

	return mcpServer
}

func (s *Service) handleListSpaces(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	params := make(map[string]string)
	if max, ok := req.GetArguments()["max"].(float64); ok {
		params["max"] = fmt.Sprintf("%.0f", max)
	}
	if t, ok := req.GetArguments()["type"].(string); ok {
		params["type"] = t
	}
	if sortBy, ok := req.GetArguments()["sortBy"].(string); ok {
		params["sortBy"] = sortBy
	} else {
		params["sortBy"] = "lastactivity"
	}

	var result struct {
		Items []map[string]any `json:"items"`
	}
	if err := s.doRequest(ctx, "GET", "/rooms", params, nil, &result); err != nil {
		return toolErrorResult("list_spaces", err), nil
	}

	s.log.Debug().Int("total_spaces", len(result.Items)).Msg("fetching room details concurrently")

	// Bulk prefetch memberships to prevent N+1 API rate limits
	_ = s.prefetchMemberships(ctx)

	type enrichedResult struct {
		space map[string]any
		keep  bool
	}
	results := make([]enrichedResult, len(result.Items))

	var wg sync.WaitGroup
	sem := make(chan struct{}, 4) // mimic browser concurrency limit (matches MaxConnsPerHost)

	for i, space := range result.Items {
		roomID, _ := space["id"].(string)
		if roomID == "" {
			continue
		}

		wg.Add(1)
		go func(i int, space map[string]any, roomID string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Unread / Membership fetch
			membership, err := s.getMembership(ctx, roomID)
			if err == nil {
				if hidden, ok := membership["isRoomHidden"].(bool); ok {
					space["isRoomHidden"] = hidden
				}
				if lastSeenId, ok := membership["lastSeenId"].(string); ok {
					space["lastSeenId"] = lastSeenId
				}
			}

			results[i] = enrichedResult{
				space: space,
				keep:  true,
			}
		}(i, space, roomID)
	}

	wg.Wait()

	enrichedSpaces := make([]map[string]any, 0) // initialized to empty array, prevents 'null'
	for _, r := range results {
		if r.keep && r.space != nil {
			enrichedSpaces = append(enrichedSpaces, r.space)
		}
	}

	return structuredResult(enrichedSpaces)
}

func (s *Service) handleGetMessages(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	roomID, ok := req.GetArguments()["room_id"].(string)
	if !ok || roomID == "" {
		return mcp.NewToolResultError("room_id is required"), nil
	}

	params := map[string]string{"roomId": roomID}
	if max, ok := req.GetArguments()["max"].(float64); ok {
		params["max"] = fmt.Sprintf("%.0f", max)
	}
	if before, ok := req.GetArguments()["before"].(string); ok {
		params["before"] = before
	}

	var result struct {
		Items []map[string]any `json:"items"`
	}
	if err := s.doRequest(ctx, "GET", "/messages", params, nil, &result); err != nil {
		return toolErrorResult("get_messages", err), nil
	}

	return structuredResult(result.Items)
}

func (s *Service) handleSendMessage(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	body := make(map[string]any)
	if roomID, ok := req.GetArguments()["room_id"].(string); ok {
		body["roomId"] = roomID
	}
	if email, ok := req.GetArguments()["to_person_email"].(string); ok {
		body["toPersonEmail"] = email
	}
	if personID, ok := req.GetArguments()["to_person_id"].(string); ok {
		body["toPersonId"] = personID
	}
	if text, ok := req.GetArguments()["text"].(string); ok {
		body["text"] = text
	}
	if markdown, ok := req.GetArguments()["markdown"].(string); ok {
		body["markdown"] = markdown
	}

	if len(body) == 0 {
		return mcp.NewToolResultError("at least one destination and one content field is required"), nil
	}

	var result map[string]any
	if err := s.doRequest(ctx, "POST", "/messages", nil, body, &result); err != nil {
		return toolErrorResult("send_message", err), nil
	}
	return structuredResult(result)
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

	membership, err := s.getMembership(ctx, roomID)
	if err != nil {
		return toolErrorResult("hide/unhide_space", err), nil
	}
	membershipID, _ := membership["id"].(string)

	body := map[string]any{"isRoomHidden": hidden}
	var result map[string]any
	if err := s.doRequest(ctx, "PUT", "/memberships/"+membershipID, nil, body, &result); err != nil {
		return toolErrorResult("hide/unhide_space", err), nil
	}
	return structuredResult(result)
}

func (s *Service) handleGetDirectMessages(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	params := make(map[string]string)
	if personID, ok := req.GetArguments()["person_id"].(string); ok {
		params["personId"] = personID
	}
	if email, ok := req.GetArguments()["person_email"].(string); ok {
		params["personEmail"] = email
	}

	if len(params) == 0 {
		return mcp.NewToolResultError("person_id or person_email is required"), nil
	}

	var result struct {
		Items []map[string]any `json:"items"`
	}
	if err := s.doRequest(ctx, "GET", "/messages/direct", params, nil, &result); err != nil {
		return toolErrorResult("get_direct_messages", err), nil
	}
	return structuredResult(result.Items)
}

type searchMatch struct {
	SpaceName string         `json:"space_name"`
	Message   map[string]any `json:"message"`
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

	// 1. List recent spaces
	spacesParams := map[string]string{
		"max":     fmt.Sprintf("%d", spaceLimit),
		"sortBy":  "lastactivity",
	}
	var spacesResult struct {
		Items []map[string]any `json:"items"`
	}
	if err := s.doRequest(ctx, "GET", "/rooms", spacesParams, nil, &spacesResult); err != nil {
		return toolErrorResult("search_messages: list_spaces", err), nil
	}

	// 2. Fetch messages from each space and filter
	var matches []searchMatch
	var matchesMu sync.Mutex
	var wg sync.WaitGroup

	queryLower := strings.ToLower(query)

	for _, space := range spacesResult.Items {
		roomID, _ := space["id"].(string)
		roomTitle, _ := space["title"].(string)
		if roomID == "" {
			continue
		}

		wg.Add(1)
		go func(roomID, roomTitle string) {
			defer wg.Done()
			params := map[string]string{
				"roomId": roomID,
				"max":    fmt.Sprintf("%d", msgLimit),
			}
			var result struct {
				Items []map[string]any `json:"items"`
			}
			// Use a separate context with a shorter timeout if needed, but for now just use the base context.
			if err := s.doRequest(ctx, "GET", "/messages", params, nil, &result); err != nil {
				s.log.Warn().Err(err).Str("room_id", roomID).Msg("failed to fetch messages for search")
				return
			}

			for _, msg := range result.Items {
				text, _ := msg["text"].(string)
				if strings.Contains(strings.ToLower(text), queryLower) {
					matchesMu.Lock()
					matches = append(matches, searchMatch{
						SpaceName: roomTitle,
						Message:   msg,
					})
					matchesMu.Unlock()
				}
			}
		}(roomID, roomTitle)
	}

	wg.Wait()

	return structuredResult(matches)
}

func (s *Service) prefetchMemberships(ctx context.Context) error {
	s.membershipsMu.Lock()
	defer s.membershipsMu.Unlock()
	if s.membershipsCache != nil {
		return nil
	}

	var result struct {
		Items []map[string]any `json:"items"`
	}
	// Fetch up to 1000 memberships at once
	params := map[string]string{"max": "1000"}
	if err := s.doRequest(ctx, "GET", "/memberships", params, nil, &result); err != nil {
		return err
	}

	s.membershipsCache = make(map[string]map[string]any)
	for _, m := range result.Items {
		roomID, _ := m["roomId"].(string)
		if roomID != "" {
			s.membershipsCache[roomID] = m
		}
	}
	return nil
}

func (s *Service) getMembership(ctx context.Context, roomID string) (map[string]any, error) {
	s.membershipsMu.Lock()
	if s.membershipsCache != nil {
		m, ok := s.membershipsCache[roomID]
		s.membershipsMu.Unlock()
		if ok {
			return m, nil
		}
		return nil, errors.New("membership not found in bulk cache")
	}
	s.membershipsMu.Unlock()

	params := map[string]string{
		"roomId":   roomID,
		"personId": "me",
	}
	var result struct {
		Items []map[string]any `json:"items"`
	}
	if err := s.doRequest(ctx, "GET", "/memberships", params, nil, &result); err != nil {
		return nil, err
	}
	if len(result.Items) == 0 {
		return nil, errors.New("membership not found for room")
	}
	return result.Items[0], nil
}

func (s *Service) doRequest(ctx context.Context, method, path string, params map[string]string, body any, result any) error {
	if s.accessToken == "" {
		return errors.New("missing Webex access token; set WEBEX_ACCESS_TOKEN or run 'clara auth webex'")
	}

	url := BaseURL + path
	if len(params) > 0 {
		var query []string
		for k, v := range params {
			query = append(query, fmt.Sprintf("%s=%s", k, v))
		}
		url += "?" + strings.Join(query, "&")
	}

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+s.accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return errors.Newf("webex API error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

func structuredResult(value any) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultStructuredOnly(value), nil
}

func toolErrorResult(tool string, err error) *mcp.CallToolResult {
	return mcp.NewToolResultError(fmt.Sprintf("%s: %v", tool, err))
}
