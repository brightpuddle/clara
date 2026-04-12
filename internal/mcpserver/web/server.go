package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
)

// Description is the human-readable summary shown in clara tool list.
const Description = "Built-in web server: search the internet and read web pages."

// Server bundles the MCP server.
type Server struct {
	mcp *server.MCPServer
	log zerolog.Logger
}

// New creates a web MCP server.
func New(log zerolog.Logger) *Server {
	s := server.NewMCPServer(
		"clara-web",
		"0.1.0",
		server.WithToolCapabilities(true),
		server.WithInstructions(Description),
	)

	srv := &Server{
		mcp: s,
		log: log.With().Str("component", "mcp_web").Logger(),
	}

	srv.registerTools()

	return srv
}

func (s *Server) registerTools() {
	s.mcp.AddTool(mcp.NewTool("search",
		mcp.WithDescription("Search the internet using DuckDuckGo. Returns top results with titles, snippets, and URLs."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("The search query string."),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of results to return (default: 5)."),
		),
	), s.handleWebSearch)

	s.mcp.AddTool(mcp.NewTool("read",
		mcp.WithDescription("Fetch and read the main text content and title of a web page."),
		mcp.WithString("url",
			mcp.Required(),
			mcp.Description("The URL of the page to read."),
		),
	), s.handleWebRead)
}

func (s *Server) handleWebSearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query, ok := req.GetArguments()["query"].(string)
	if !ok || query == "" {
		return mcp.NewToolResultError("missing required argument: query"), nil
	}

	limit := 5
	if l, ok := req.GetArguments()["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}

	s.log.Info().Str("query", query).Int("limit", limit).Msg("performing web search")

	results, err := s.scrapeDDG(query, limit)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("web search failed: %v", err)), nil
	}

	// Format results for the LLM
	var output strings.Builder
	for i, res := range results {
		fmt.Fprintf(&output, "[%d] %s\nURL: %s\nSnippet: %s\n\n", i+1, res.Title, res.URL, res.Snippet)
	}

	if output.Len() == 0 {
		return mcp.NewToolResultText("No results found."), nil
	}

	return mcp.NewToolResultText(strings.TrimSpace(output.String())), nil
}

func (s *Server) handleWebRead(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	u, ok := req.GetArguments()["url"].(string)
	if !ok || u == "" {
		return mcp.NewToolResultError("missing required argument: url"), nil
	}

	s.log.Info().Str("url", u).Msg("reading web page")

	title, content, err := s.fetchPage(u)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to read page: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Title: %s\n\nContent:\n%s", title, content)), nil
}

type searchResult struct {
	Title   string
	URL     string
	Snippet string
}

func (s *Server) scrapeDDG(query string, limit int) ([]searchResult, error) {
	searchURL := fmt.Sprintf("https://lite.duckduckgo.com/lite/?q=%s", url.QueryEscape(query))
	
	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.114 Safari/537.36")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("duckduckgo returned status code %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	var results []searchResult
	doc.Find("table").Last().Find("tr").Each(func(i int, sel *goquery.Selection) {
		link := sel.Find("a.result-link")
		if link.Length() > 0 && len(results) < limit {
			title := strings.TrimSpace(link.Text())
			href, _ := link.Attr("href")
			snippet := strings.TrimSpace(sel.Next().Find("td.result-snippet").Text())
			results = append(results, searchResult{
				Title:   title,
				URL:     href,
				Snippet: snippet,
			})
		}
	})

	if len(results) == 0 {
		doc.Find(".result-link").Each(func(i int, sel *goquery.Selection) {
			if len(results) >= limit {
				return
			}
			title := strings.TrimSpace(sel.Text())
			href, _ := sel.Attr("href")
			snippet := strings.TrimSpace(sel.Closest("tr").Next().Text())
			results = append(results, searchResult{
				Title: title,
				URL: href,
				Snippet: snippet,
			})
		})
	}

	return results, nil
}

func (s *Server) fetchPage(u string) (string, string, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return "", "", err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.114 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("server returned status code %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", "", err
	}

	title := strings.TrimSpace(doc.Find("title").Text())

	// Remove scripts, styles, and nav elements to clean up content
	doc.Find("script, style, nav, footer, header, aside").Remove()

	// Try to find the "main" content
	content := ""
	main := doc.Find("main, article, #content, .content, .post")
	if main.Length() > 0 {
		content = strings.TrimSpace(main.Text())
	} else {
		content = strings.TrimSpace(doc.Find("body").Text())
	}

	// Basic whitespace cleanup
	lines := strings.Split(content, "\n")
	var cleanedLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			cleanedLines = append(cleanedLines, trimmed)
		}
	}
	
	finalContent := strings.Join(cleanedLines, "\n")
	// Cap content to a reasonable size for LLM context
	if len(finalContent) > 20000 {
		finalContent = finalContent[:20000] + "... [truncated]"
	}

	return title, finalContent, nil
}

// NewServer returns the underlying MCPServer for use with ServeStdio.
func (s *Server) NewServer() *server.MCPServer {
	return s.mcp
}
