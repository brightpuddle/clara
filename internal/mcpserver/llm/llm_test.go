package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/brightpuddle/clara/internal/mcpserver/llm/providers"
)

func TestGenerateUsesGeminiProvider(t *testing.T) {
	var (
		mu         sync.Mutex
		requestErr error
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v1beta/models/test-model:generateContent" {
			mu.Lock()
			requestErr = errors.Newf("unexpected request path: %s", got)
			mu.Unlock()
			http.Error(w, requestErr.Error(), http.StatusBadRequest)
			return
		}
		if got := r.URL.Query().Get("key"); got != "test-key" {
			mu.Lock()
			requestErr = errors.Newf("unexpected API key query: %q", got)
			mu.Unlock()
			http.Error(w, requestErr.Error(), http.StatusBadRequest)
			return
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			mu.Lock()
			requestErr = err
			mu.Unlock()
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		contents := body["contents"].([]any)
		first := contents[0].(map[string]any)
		parts := first["parts"].([]any)
		prompt := parts[0].(map[string]any)["text"]
		if prompt != "Hello world" {
			mu.Lock()
			requestErr = errors.Newf("unexpected prompt: %#v", prompt)
			mu.Unlock()
			http.Error(w, requestErr.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"candidates": [
			  {
			    "content": {"parts": [{"text": "Hello from Gemini"}]},
			    "finishReason": "STOP"
			  }
			]
		}`))
	}))
	defer server.Close()

	svc := New(Options{
		GeminiAPIKey:  "test-key",
		GeminiModel:   "test-model",
		GeminiBaseURL: server.URL,
	})

	result, err := svc.handleGenerate(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"prompt": "Hello world",
		}},
	})
	if err != nil {
		t.Fatalf("handleGenerate returned error: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if requestErr != nil {
		t.Fatalf("request validation failed: %v", requestErr)
	}
	if result.IsError {
		t.Fatalf("expected success result, got error: %#v", result.Content)
	}

	content, ok := result.StructuredContent.(providers.GenerateResult)
	if !ok {
		contentMap, ok := result.StructuredContent.(map[string]any)
		if !ok {
			t.Fatalf("unexpected structured content: %#v", result.StructuredContent)
		}
		if contentMap["text"] != "Hello from Gemini" {
			t.Fatalf("unexpected text result: %#v", contentMap)
		}
		return
	}
	if content.Text != "Hello from Gemini" {
		t.Fatalf("unexpected text result: %#v", content)
	}
}

func TestGenerateVisionSendsInlineImageData(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "shirt.jpg")
	imageData := []byte("fake-jpeg-data")
	if err := os.WriteFile(imagePath, imageData, 0o644); err != nil {
		t.Fatalf("write image file: %v", err)
	}

	var (
		mu         sync.Mutex
		requestErr error
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			mu.Lock()
			requestErr = err
			mu.Unlock()
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		contents := body["contents"].([]any)
		first := contents[0].(map[string]any)
		parts := first["parts"].([]any)
		if len(parts) != 2 {
			mu.Lock()
			requestErr = errors.Newf("expected prompt plus image parts, got %#v", parts)
			mu.Unlock()
			http.Error(w, requestErr.Error(), http.StatusBadRequest)
			return
		}
		inlineData := parts[1].(map[string]any)["inline_data"].(map[string]any)
		if inlineData["mime_type"] != "image/jpeg" {
			mu.Lock()
			requestErr = errors.Newf("unexpected mime type: %#v", inlineData)
			mu.Unlock()
			http.Error(w, requestErr.Error(), http.StatusBadRequest)
			return
		}
		if inlineData["data"] != base64.StdEncoding.EncodeToString(imageData) {
			mu.Lock()
			requestErr = errors.Newf("unexpected inline data payload: %#v", inlineData)
			mu.Unlock()
			http.Error(w, requestErr.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"candidates": [
			  {
			    "content": {"parts": [{"text": "Blue shirt"}]},
			    "finishReason": "STOP"
			  }
			]
		}`))
	}))
	defer server.Close()

	svc := New(Options{
		GeminiAPIKey:  "test-key",
		GeminiModel:   "test-model",
		GeminiBaseURL: server.URL,
	})

	result, err := svc.handleGenerateVision(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"prompt": "Describe this item",
			"images": []any{imagePath},
		}},
	})
	if err != nil {
		t.Fatalf("handleGenerateVision returned error: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if requestErr != nil {
		t.Fatalf("request validation failed: %v", requestErr)
	}
	if result.IsError {
		t.Fatalf("expected success result, got error: %#v", result.Content)
	}

	contentMap, ok := result.StructuredContent.(map[string]any)
	if ok {
		if contentMap["text"] != "Blue shirt" {
			t.Fatalf("unexpected text result: %#v", contentMap)
		}
	}
}

func TestProvidersReportsGeminiConfigurationState(t *testing.T) {
	svc := New(Options{})

	result, err := svc.handleProviders(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handleProviders returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success result, got error: %#v", result.Content)
	}

	providersList, ok := result.StructuredContent.([]map[string]any)
	if !ok {
		rows, ok := result.StructuredContent.([]any)
		if !ok || len(rows) != 1 {
			t.Fatalf("unexpected providers result: %#v", result.StructuredContent)
		}
		row, _ := rows[0].(map[string]any)
		if row["status"] != "unconfigured" {
			t.Fatalf("unexpected provider status: %#v", row)
		}
		return
	}
	if len(providersList) != 1 || providersList[0]["status"] != "unconfigured" {
		t.Fatalf("unexpected providers result: %#v", providersList)
	}
}
