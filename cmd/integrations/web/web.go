package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/brightpuddle/clara/pkg/contract"
	"github.com/mark3labs/mcp-go/mcp"
)

type Web struct{}

func (w *Web) Configure(config []byte) error {
	return nil
}

func (w *Web) Description() (string, error) {
	return "Built-in web integration: search the internet and read web pages.", nil
}

func (w *Web) Tools() ([]byte, error) {
	return json.Marshal([]mcp.Tool{
		mcp.NewTool(
			"search",
			mcp.WithDescription("Search the internet using DuckDuckGo. Returns top results with titles, snippets, and URLs."),
			mcp.WithString("query", mcp.Required(), mcp.Description("The search query string.")),
			mcp.WithNumber("limit", mcp.Description("Maximum number of results to return (default: 5).")),
		),
		mcp.NewTool(
			"read",
			mcp.WithDescription("Fetch and read the main text content and title of a web page."),
			mcp.WithString("url", mcp.Required(), mcp.Description("The URL of the page to read.")),
		),
	})
}

type SearchArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type ReadArgs struct {
	URL string `json:"url"`
}

func (w *Web) CallTool(name string, args []byte) ([]byte, error) {
	switch name {
	case "search":
		var parsed SearchArgs
		if err := json.Unmarshal(args, &parsed); err != nil {
			return nil, err
		}
		if parsed.Limit <= 0 {
			parsed.Limit = 5
		}
		results, err := w.Search(parsed.Query, parsed.Limit)
		if err != nil {
			return nil, err
		}
		var output strings.Builder
		for i, res := range results {
			fmt.Fprintf(&output, "[%d] %s\nURL: %s\nSnippet: %s\n\n", i+1, res.Title, res.URL, res.Snippet)
		}
		if output.Len() == 0 {
			return json.Marshal("No results found.")
		}
		return json.Marshal(strings.TrimSpace(output.String()))

	case "read":
		var parsed ReadArgs
		if err := json.Unmarshal(args, &parsed); err != nil {
			return nil, err
		}
		content, err := w.Read(parsed.URL)
		if err != nil {
			return nil, err
		}
		return json.Marshal(content)
	}
	return nil, fmt.Errorf("unknown tool: %s", name)
}

func (w *Web) Search(query string, limit int) ([]contract.SearchResult, error) {
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

	var results []contract.SearchResult
	doc.Find("table").Last().Find("tr").Each(func(i int, sel *goquery.Selection) {
		link := sel.Find("a.result-link")
		if link.Length() > 0 && len(results) < limit {
			title := strings.TrimSpace(link.Text())
			href, _ := link.Attr("href")
			snippet := strings.TrimSpace(sel.Next().Find("td.result-snippet").Text())
			results = append(results, contract.SearchResult{
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
			results = append(results, contract.SearchResult{
				Title:   title,
				URL:     href,
				Snippet: snippet,
			})
		})
	}

	return results, nil
}

func (w *Web) Read(u string) (string, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.114 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned status code %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", err
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

	return fmt.Sprintf("Title: %s\n\nContent:\n%s", title, finalContent), nil
}
