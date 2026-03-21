package chrome

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
)

// Description is the human-readable summary shown in clara tool list.
const Description = "Built-in Chrome browser automation: navigate, click, fill, " +
	"upload files, read page content, screenshot, and manage tabs."

// DefaultPort is the localhost port the bridge listens on for extension connections.
// The Chrome extension must be configured to connect to ws://localhost:DefaultPort.
const DefaultPort = 48765

// Server bundles the MCP server with its underlying WebSocket bridge. Use New
// to construct and Run to start both concurrently.
type Server struct {
	bridge *Bridge
	mcp    *server.MCPServer
	log    zerolog.Logger
}

// New creates a Chrome MCP server. Call Run to start serving.
func New(log zerolog.Logger) *Server {
	b := newBridge(log)
	s := server.NewMCPServer(
		"clara-chrome",
		"0.1.0",
		server.WithToolCapabilities(true),
	)
	registerTools(s, b)
	return &Server{bridge: b, mcp: s, log: log}
}

// Run starts the WebSocket bridge on localhost:port concurrently with the MCP
// stdio server. It blocks until ctx is cancelled or either server fails.
func (s *Server) Run(ctx context.Context, port int) error {
	addr := fmt.Sprintf("localhost:%d", port)

	bridgeErrCh := make(chan error, 1)
	go func() {
		bridgeErrCh <- s.bridge.Serve(ctx, addr)
	}()

	mcpErrCh := make(chan error, 1)
	go func() {
		mcpErrCh <- server.ServeStdio(s.mcp)
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-bridgeErrCh:
		return err
	case err := <-mcpErrCh:
		return err
	}
}

// ── Tool registration ─────────────────────────────────────────────────────────

func registerTools(s *server.MCPServer, b *Bridge) {
	// browser_navigate
	s.AddTool(mcp.NewTool("browser_navigate",
		mcp.WithDescription(
			"Navigate to a URL. Opens a new background tab by default. "+
				"Returns the tab_id and final URL.",
		),
		mcp.WithString("url",
			mcp.Required(),
			mcp.Description("URL to navigate to."),
		),
		mcp.WithNumber("tab_id",
			mcp.Description("Existing tab ID to navigate. If omitted, a new tab is opened."),
		),
		mcp.WithBoolean("background",
			mcp.Description(
				"When true (default), the new tab is opened in the background "+
					"without stealing focus.",
			),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invoke(ctx, b, "navigate", req.GetArguments())
	})

	// browser_click
	s.AddTool(mcp.NewTool("browser_click",
		mcp.WithDescription(
			"Click an element identified by a CSS selector. "+
				"Scrolls the element into view and dispatches a full click sequence.",
		),
		mcp.WithNumber("tab_id",
			mcp.Required(),
			mcp.Description("ID of the tab that contains the element."),
		),
		mcp.WithString("selector",
			mcp.Required(),
			mcp.Description("CSS selector for the element to click."),
		),
		mcp.WithNumber("wait_after_ms",
			mcp.Description(
				"Milliseconds to wait after clicking (default 500). "+
					"Increase for pages with animated transitions.",
			),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invoke(ctx, b, "click", req.GetArguments())
	})

	// browser_fill
	s.AddTool(mcp.NewTool("browser_fill",
		mcp.WithDescription(
			"Fill a text input or textarea identified by a CSS selector. "+
				"Uses the React-compatible native value setter so controlled components update correctly.",
		),
		mcp.WithNumber("tab_id",
			mcp.Required(),
			mcp.Description("ID of the tab that contains the element."),
		),
		mcp.WithString("selector",
			mcp.Required(),
			mcp.Description("CSS selector for the input element to fill."),
		),
		mcp.WithString("value",
			mcp.Required(),
			mcp.Description("Text value to set."),
		),
		mcp.WithBoolean("clear_first",
			mcp.Description(
				"Select all existing text before typing (default true).",
			),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invoke(ctx, b, "fill", req.GetArguments())
	})

	// browser_upload_file
	s.AddTool(mcp.NewTool("browser_upload_file",
		mcp.WithDescription(
			"Set a local file on a <input type=\"file\"> element using the Chrome "+
				"DevTools Protocol. The file_path must be an absolute path accessible "+
				"on the local machine.",
		),
		mcp.WithNumber("tab_id",
			mcp.Required(),
			mcp.Description("ID of the tab that contains the file input."),
		),
		mcp.WithString("selector",
			mcp.Required(),
			mcp.Description("CSS selector for the <input type=\"file\"> element."),
		),
		mcp.WithString("file_path",
			mcp.Required(),
			mcp.Description("Absolute path to the local file to upload."),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invoke(ctx, b, "upload_file", req.GetArguments())
	})

	// browser_screenshot
	s.AddTool(mcp.NewTool("browser_screenshot",
		mcp.WithDescription(
			"Capture a PNG screenshot of the visible area of a tab. "+
				"Returns a data URL (data:image/png;base64,...).",
		),
		mcp.WithNumber("tab_id",
			mcp.Description("ID of the tab to screenshot. If omitted, uses the active tab."),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invoke(ctx, b, "screenshot", req.GetArguments())
	})

	// browser_read_page
	s.AddTool(mcp.NewTool("browser_read_page",
		mcp.WithDescription(
			"Read the visible text content, title, and URL of a tab. "+
				"Useful for extracting information from a loaded page.",
		),
		mcp.WithNumber("tab_id",
			mcp.Required(),
			mcp.Description("ID of the tab to read."),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invoke(ctx, b, "read_page", req.GetArguments())
	})

	// browser_get_tabs
	s.AddTool(mcp.NewTool("browser_get_tabs",
		mcp.WithDescription(
			"List open browser tabs. Optionally filter by URL pattern.",
		),
		mcp.WithString("url_filter",
			mcp.Description(
				"Optional URL match pattern (e.g. '*://facebook.com/*'). "+
					"If omitted, all tabs are returned.",
			),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invoke(ctx, b, "get_tabs", req.GetArguments())
	})

	// browser_close_tab
	s.AddTool(mcp.NewTool("browser_close_tab",
		mcp.WithDescription("Close a browser tab by ID."),
		mcp.WithNumber("tab_id",
			mcp.Required(),
			mcp.Description("ID of the tab to close."),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invoke(ctx, b, "close_tab", req.GetArguments())
	})

	// browser_wait_for_load
	s.AddTool(mcp.NewTool("browser_wait_for_load",
		mcp.WithDescription(
			"Wait until a tab's document status is 'complete'. "+
				"Use after browser_navigate to ensure the page has fully loaded "+
				"before reading or interacting with it.",
		),
		mcp.WithNumber("tab_id",
			mcp.Required(),
			mcp.Description("ID of the tab to wait on."),
		),
		mcp.WithNumber("timeout_seconds",
			mcp.Description("Maximum seconds to wait (default 30)."),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invoke(ctx, b, "wait_for_load", req.GetArguments())
	})
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// invoke sends a tool call to the extension and formats the result as an MCP
// text content block. All MCP arguments are forwarded to the extension as-is.
func invoke(
	ctx context.Context,
	b *Bridge,
	tool string,
	args map[string]any,
) (*mcp.CallToolResult, error) {
	raw, err := b.callTool(ctx, tool, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if raw == nil || string(raw) == "null" {
		return mcp.NewToolResultText(`{"ok":true}`), nil
	}
	// Pretty-print the JSON for readability in intent logs.
	var v any
	if jsonErr := json.Unmarshal(raw, &v); jsonErr != nil {
		return mcp.NewToolResultText(string(raw)), nil
	}
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultText(string(raw)), nil
	}
	return mcp.NewToolResultText(string(pretty)), nil
}
