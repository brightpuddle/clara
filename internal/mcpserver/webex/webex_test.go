package webex

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"
)

type mockTransport struct {
	roundTrip func(req *http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTrip(req)
}

func TestService_SearchMessages(t *testing.T) {
	mock := &mockTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			var resp any
			switch {
			case req.URL.Path == "/v1/rooms":
				resp = map[string]any{
					"items": []map[string]any{
						{"id": "room1", "title": "Room One"},
						{"id": "room2", "title": "Room Two"},
					},
				}
			case req.URL.Path == "/v1/messages" && req.URL.Query().Get("roomId") == "room1":
				resp = map[string]any{
					"items": []map[string]any{
						{"text": "Hello world"},
						{"text": "Secret keyword found"},
					},
				}
			case req.URL.Path == "/v1/messages" && req.URL.Query().Get("roomId") == "room2":
				resp = map[string]any{
					"items": []map[string]any{
						{"text": "No keyword here"},
					},
				}
			default:
				resp = map[string]any{"items": []any{}}
			}

			b, _ := json.Marshal(resp)
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader(b)),
				Header:     make(http.Header),
			}, nil
		},
	}

	httpClient := &http.Client{Transport: mock}
	s := New("fake-token", httpClient, zerolog.Nop())

	req := mcp.CallToolRequest{}
	req.Params.Name = "search_messages"
	req.Params.Arguments = map[string]any{
		"query": "secret",
	}

	res, err := s.handleSearchMessages(context.Background(), req)
	if err != nil {
		t.Fatalf("handleSearchMessages: %v", err)
	}

	if res.IsError {
		t.Fatalf("tool error: %v", res.Content[0].(*mcp.TextContent).Text)
	}

	// StructuredContent should contain the matches
	matches, ok := res.StructuredContent.([]searchMatch)
	if !ok {
		t.Fatalf("expected []searchMatch result, got %T", res.StructuredContent)
	}

	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}

	m := matches[0]
	if m.SpaceName != "Room One" {
		t.Errorf("expected Room One, got %v", m.SpaceName)
	}
	msg := m.Message
	if msg.Text != "Secret keyword found" {
		t.Errorf("expected 'Secret keyword found', got %v", msg.Text)
	}
}
