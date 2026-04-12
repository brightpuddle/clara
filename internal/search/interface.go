package search

import (
	"context"
	"fmt"
	"github.com/mark3labs/mcp-go/mcp"
)

// Searcher is the interface that all search-capable MCP servers should implement.
type Searcher interface {
	Search(ctx context.Context, query string, options SearchOptions) ([]Result, error)
}

// SearchOptions provides common filtering and scoping for search.
type SearchOptions struct {
	Limit    int
	Offset   int
	Metadata map[string]string // Any additional implementation-specific options
}

// Result is a common search result structure.
type Result struct {
	ID          string            `json:"id"`
	Title       string            `json:"title,omitempty"`
	Description string            `json:"description,omitempty"`
	Score       float64           `json:"score"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// ResultToMCPContent converts a search result into MCP content for a tool response.
func ResultToMCPContent(res Result) mcp.Content {
	// We'll return it as JSON for now, or text depending on needs.
	return mcp.NewTextContent(fmt.Sprintf("[%s] %s\n%s (Score: %.2f)", res.ID, res.Title, res.Description, res.Score))
}

// Formatter is a helper to format multiple results for MCP.
func FormatResults(results []Result) []mcp.Content {
	var contents []mcp.Content
	for _, res := range results {
		contents = append(contents, ResultToMCPContent(res))
	}
	return contents
}
