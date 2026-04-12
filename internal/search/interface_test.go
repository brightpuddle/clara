package search

import (
	"testing"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestFormatResults(t *testing.T) {
	results := []Result{
		{
			ID: "test-1",
			Title: "Test Title",
			Description: "Test Description",
			Score: 1.0,
		},
	}
	
	contents := FormatResults(results)
	if len(contents) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(contents))
	}
	
	tc, ok := contents[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", contents[0])
	}
	
	expected := "[test-1] Test Title\nTest Description (Score: 1.00)"
	if tc.Text != expected {
		t.Errorf("expected %q, got %q", expected, tc.Text)
	}
}
