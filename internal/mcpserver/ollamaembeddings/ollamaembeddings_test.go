package ollamaembeddings

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestEmbedInputs(t *testing.T) {
	inputs, err := embedInputs(map[string]any{"input": "hello"})
	if err != nil {
		t.Fatalf("embedInputs single: %v", err)
	}
	if len(inputs) != 1 || inputs[0] != "hello" {
		t.Fatalf("unexpected single inputs: %#v", inputs)
	}

	inputs, err = embedInputs(map[string]any{"inputs": []any{"a", "b"}})
	if err != nil {
		t.Fatalf("embedInputs batch: %v", err)
	}
	if len(inputs) != 2 || inputs[1] != "b" {
		t.Fatalf("unexpected batch inputs: %#v", inputs)
	}
}

func TestServiceEmbedViaAPIEmbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload["model"] != "model-x" {
			t.Fatalf("unexpected model: %#v", payload["model"])
		}
		json.NewEncoder(w).Encode(map[string]any{
			"embeddings": [][]float64{{1, 2}, {3, 4}},
		}) //nolint:errcheck
	}))
	defer srv.Close()

	service := New(srv.URL, "model-x")
	embeddings, err := service.embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(embeddings) != 2 || embeddings[1][1] != 4 {
		t.Fatalf("unexpected embeddings: %#v", embeddings)
	}
}

func TestServiceEmbedFallsBackToLegacyAPI(t *testing.T) {
	var legacyCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/embed":
			http.NotFound(w, r)
		case "/api/embeddings":
			legacyCalls++
			json.NewEncoder(w).Encode(map[string]any{
				"embedding": []float64{float64(legacyCalls), 42},
			}) //nolint:errcheck
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	service := New(srv.URL, "")
	embeddings, err := service.embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("embed fallback: %v", err)
	}
	if len(embeddings) != 2 || embeddings[0][0] != 1 || embeddings[1][0] != 2 {
		t.Fatalf("unexpected fallback embeddings: %#v", embeddings)
	}
}

func TestHandleEmbedReturnsStructuredResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"embedding": []float64{0.1, 0.2},
		}) //nolint:errcheck
	}))
	defer srv.Close()

	service := New(srv.URL, DefaultModel)
	result, err := service.handleEmbed(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]any{"input": "hello"},
		},
	})
	if err != nil {
		t.Fatalf("handleEmbed: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected MCP error result: %#v", result)
	}
	if result.StructuredContent == nil {
		t.Fatal("expected structured content")
	}
}
