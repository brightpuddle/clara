package main

import (
	"testing"

	"github.com/brightpuddle/clara/internal/registry"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestGatewayToolResultText(t *testing.T) {
	result, err := registry.FormatToolResult("hello")
	if err != nil {
		t.Fatalf("gatewayToolResult returned error: %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	if text.Text != "hello" {
		t.Fatalf("text = %q, want hello", text.Text)
	}
	if result.StructuredContent != nil {
		t.Fatalf("expected nil structured content, got %#v", result.StructuredContent)
	}
}

func TestGatewayToolResultStructured(t *testing.T) {
	payload := map[string]any{"name": "clara", "ok": true}

	result, err := registry.FormatToolResult(payload)
	if err != nil {
		t.Fatalf("gatewayToolResult returned error: %v", err)
	}
	if result.StructuredContent == nil {
		t.Fatal("expected structured content")
	}
	if got := result.StructuredContent.(map[string]any)["name"]; got != "clara" {
		t.Fatalf("structured content name = %v, want clara", got)
	}
}

func TestGatewayCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"mcpserver", "run"})
	if err != nil {
		t.Fatalf("Find mcpserver run command: %v", err)
	}
	if cmd == nil || cmd.Name() != "run" {
		t.Fatalf("expected mcpserver run command, got %#v", cmd)
	}
}
