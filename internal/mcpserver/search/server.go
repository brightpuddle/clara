package search

import (
	"context"
	"fmt"

	"github.com/brightpuddle/clara/internal/search"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
)

type Server struct {
	*server.MCPServer
	log     zerolog.Logger
	indexer *search.Indexer
}

func New(indexPath string, log zerolog.Logger) (*Server, error) {
	schema := &search.IndexSchema{
		Name:    "mail",
		Columns: []string{"subject", "from", "to", "body", "date", "message_id"},
	}
	
	indexer, err := search.NewIndexer(indexPath, schema)
	if err != nil {
		return nil, fmt.Errorf("init mail indexer: %w", err)
	}

	s := &Server{
		log:     log.With().Str("component", "mcp_search").Logger(),
		indexer: indexer,
	}

	s.MCPServer = server.NewMCPServer(
		"clara-search",
		"0.1.0",
		server.WithToolCapabilities(true),
		server.WithInstructions("Built-in search server: provides aggregated search across namespaces."),
	)

	s.AddTool(mcp.NewTool("mail.search",
		mcp.WithDescription("Search for emails indexed from ~/Library/Mail using SQLite FTS5."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("FTS5 search query."),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of results to return."),
		),
	), s.handleMailSearch)

	return s, nil
}

func (s *Server) handleMailSearch(
	ctx context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	query, err := stringArg(req, "query")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	limit := intArg(req, "limit")

	results, err := s.indexer.Search(ctx, query, limit)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("mail search: %v", err)), nil
	}

	// For now, just return the IDs (which will be file paths or message IDs)
	output := []string{}
	for _, res := range results {
		output = append(output, res.ID)
	}

	result, err := mcp.NewToolResultJSON(output)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal results: %v", err)), nil
	}
	return result, nil
}

func stringArg(req mcp.CallToolRequest, name string) (string, error) {
	val, ok := req.GetArguments()[name]
	if !ok {
		return "", fmt.Errorf("missing required argument: %q", name)
	}
	s, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string", name)
	}
	return s, nil
}

func intArg(req mcp.CallToolRequest, name string) int {
	val, ok := req.GetArguments()[name]
	if !ok {
		return 0
	}
	switch v := val.(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}
