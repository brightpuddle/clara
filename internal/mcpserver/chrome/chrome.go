package chrome

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"

	clara "github.com/brightpuddle/clara"
)

// Description is the human-readable summary shown in clara tool list.
const Description = "Built-in Chrome browser automation: navigate, click, fill, " +
	"upload files, read page content, screenshot, and manage tabs."

// Server bundles the MCP server with its underlying UDS bridge. Use New
// to construct and Run to start both concurrently.
type Server struct {
	bridge *Bridge
	mcp    *server.MCPServer
	log    zerolog.Logger
}

// New creates a Chrome MCP server. Call Run to start serving.
func New(log zerolog.Logger) *Server {
	ver := embeddedExtensionVersion()
	b := newBridge(log, ver, func() error {
		return updateExtensionFiles()
	})
	s := server.NewMCPServer(
		"clara-chrome",
		"0.1.0",
		server.WithToolCapabilities(true),
		server.WithInstructions(Description),
	)
	registerTools(s, b)
	return &Server{bridge: b, mcp: s, log: log}
}

// Run starts the UDS bridge concurrently with the MCP stdio server.
// It blocks until ctx is cancelled or either server fails.
func (s *Server) Run(ctx context.Context) error {
	bridgeErrCh := make(chan error, 1)
	go func() {
		bridgeErrCh <- s.bridge.Serve(ctx)
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

// embeddedExtensionVersion reads the version field from the extension
// manifest.json that is embedded in the binary.
func embeddedExtensionVersion() string {
	data, err := clara.ExtensionFS.ReadFile("extension/manifest.json")
	if err != nil {
		return ""
	}
	var m struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}
	return m.Version
}

// extensionDir returns the canonical path where extension files are written
// when performing an auto-update. It mirrors the default used by
// `clara chrome update-extension`.
func extensionDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "clara", "extension")
}

// updateExtensionFiles writes the embedded extension files to extensionDir().
func updateExtensionFiles() error {
	target := extensionDir()
	if err := os.MkdirAll(target, 0755); err != nil {
		return err
	}
	return fs.WalkDir(
		clara.ExtensionFS,
		"extension",
		func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel("extension", path)
			if err != nil {
				return err
			}
			if rel == "." {
				return nil
			}
			dest := filepath.Join(target, rel)
			if d.IsDir() {
				return os.MkdirAll(dest, 0755)
			}
			srcFile, err := clara.ExtensionFS.Open(path)
			if err != nil {
				return err
			}
			defer srcFile.Close()
			destFile, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
			if err != nil {
				return err
			}
			defer destFile.Close()
			_, err = io.Copy(destFile, srcFile)
			return err
		},
	)
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

	// browser_fill_by_label
	s.AddTool(mcp.NewTool("browser_fill_by_label",
		mcp.WithDescription(
			"Find a text input or textarea by its label text and fill it. "+
				"Searches for an element containing the label text and looks for the nearest input/textarea.",
		),
		mcp.WithNumber("tab_id",
			mcp.Required(),
			mcp.Description("ID of the tab that contains the element."),
		),
		mcp.WithString("label",
			mcp.Required(),
			mcp.Description("Label text to search for."),
		),
		mcp.WithString("value",
			mcp.Required(),
			mcp.Description("Text value to set."),
		),
		mcp.WithString("tag",
			mcp.Description("HTML tag of the input element: 'input' (default) or 'textarea'."),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invoke(ctx, b, "fill_by_label", req.GetArguments())
	})

	// browser_click_by_label
	s.AddTool(mcp.NewTool("browser_click_by_label",
		mcp.WithDescription(
			"Find a button, link, or clickable element by its text and click it.",
		),
		mcp.WithNumber("tab_id",
			mcp.Required(),
			mcp.Description("ID of the tab that contains the element."),
		),
		mcp.WithString("label",
			mcp.Required(),
			mcp.Description("Text or aria-label to search for."),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invoke(ctx, b, "click_by_label", req.GetArguments())
	})

	// browser_upload_file
	s.AddTool(mcp.NewTool("browser_upload_file",
		mcp.WithDescription(
			"Set one or more local files on a <input type=\"file\"> element using "+
				"the Chrome DevTools Protocol. Paths must be absolute and accessible on "+
				"the local machine.",
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
			mcp.Description("Absolute path to one local file to upload."),
		),
		mcp.WithArray(
			"file_paths",
			mcp.Description("Optional array of absolute paths to upload in one selection."),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invoke(ctx, b, "upload_file", req.GetArguments())
	})

	// browser_eval
	s.AddTool(mcp.NewTool("browser_eval",
		mcp.WithDescription(
			"Execute an async JavaScript snippet in the page context and return its JSON-serializable result.",
		),
		mcp.WithNumber("tab_id",
			mcp.Required(),
			mcp.Description("ID of the tab where the script should run."),
		),
		mcp.WithString(
			"script",
			mcp.Required(),
			mcp.Description(
				"JavaScript function body executed as async code with `args` in scope.",
			),
		),
		mcp.WithObject(
			"args",
			mcp.Description("Optional JSON-serializable argument object passed into the script."),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invoke(ctx, b, "eval", req.GetArguments())
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
				"Optional URL match pattern (e.g. '*://example.com/*'). "+
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

	// browser_cleanup_tabs
	s.AddTool(mcp.NewTool("browser_cleanup_tabs",
		mcp.WithDescription("Close all browser tabs that were opened by Clara automation."),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invoke(ctx, b, "cleanup_tabs", req.GetArguments())
	})

	// browser_wait_for_selector
	s.AddTool(mcp.NewTool("browser_wait_for_selector",
		mcp.WithDescription(
			"Wait until an element matching a CSS selector is present in the DOM.",
		),
		mcp.WithNumber("tab_id",
			mcp.Required(),
			mcp.Description("ID of the tab to search in."),
		),
		mcp.WithString("selector",
			mcp.Required(),
			mcp.Description("CSS selector to wait for."),
		),
		mcp.WithNumber("timeout_seconds",
			mcp.Description("Maximum seconds to wait (default 30)."),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invoke(ctx, b, "wait_for_selector", req.GetArguments())
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

	// browser_query_elements
	s.AddTool(mcp.NewTool("browser_query_elements",
		mcp.WithDescription(
			"Query multiple elements by CSS selector and return their attributes (tag, id, class, text, value, etc). "+
				"Bypasses CSP restrictions that block browser_eval.",
		),
		mcp.WithNumber("tab_id",
			mcp.Required(),
			mcp.Description("ID of the tab to search in."),
		),
		mcp.WithString("selector",
			mcp.Required(),
			mcp.Description("CSS selector to query."),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invoke(ctx, b, "query_elements", req.GetArguments())
	})

	// browser_type
	s.AddTool(mcp.NewTool("browser_type",
		mcp.WithDescription(
			"Simulate character-by-character typing into the currently focused element using the Chrome Debugger Protocol. "+
				"Ensures React and other framework state is correctly updated. Use browser_click first to focus the desired input.",
		),
		mcp.WithNumber("tab_id",
			mcp.Required(),
			mcp.Description("ID of the tab to type in."),
		),
		mcp.WithString("text",
			mcp.Required(),
			mcp.Description("Text to type."),
		),
		mcp.WithNumber("delay_between_keys_ms",
			mcp.Description("Optional delay between keystrokes (default 10ms)."),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invoke(ctx, b, "type", req.GetArguments())
	})

	// browser_debugger_command
	s.AddTool(mcp.NewTool("browser_debugger_command",
		mcp.WithDescription(
			"Directly execute a Chrome DevTools Protocol (CDP) command on a tab. "+
				"Advanced usage only.",
		),
		mcp.WithNumber("tab_id",
			mcp.Required(),
			mcp.Description("ID of the tab to target."),
		),
		mcp.WithString("method",
			mcp.Required(),
			mcp.Description("CDP method name (e.g. 'Input.dispatchKeyEvent')."),
		),
		mcp.WithObject("params",
			mcp.Description("Optional CDP parameters object."),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invoke(ctx, b, "debugger_command", req.GetArguments())
	})

	// browser_type_by_selector
	s.AddTool(mcp.NewTool("browser_type_by_selector",
		mcp.WithDescription(
			"Type text into an element identified by CSS selector using native CDP commands. "+
				"Handles focus and character dispatch in a single session for maximum reliability with React.",
		),
		mcp.WithNumber("tab_id",
			mcp.Required(),
			mcp.Description("ID of the tab to target."),
		),
		mcp.WithString("selector",
			mcp.Required(),
			mcp.Description("CSS selector of the element to type into."),
		),
		mcp.WithString("text",
			mcp.Required(),
			mcp.Description("Text to type."),
		),
		mcp.WithNumber("delay_between_keys_ms",
			mcp.Description("Optional delay between keystrokes (default 10ms)."),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return invoke(ctx, b, "type_by_selector", req.GetArguments())
	})

	// chrome_extension_status
	s.AddTool(mcp.NewTool("chrome_extension_status",
		mcp.WithDescription(
			"Check if the Clara Chrome extension is currently connected to the bridge.",
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		connected := b.IsConnected()
		return mcp.NewToolResultText(fmt.Sprintf(`{"connected": %t}`, connected)), nil
	})

	// browser_reload_extension
	s.AddTool(mcp.NewTool("browser_reload_extension",
		mcp.WithDescription(
			"Signal the Chrome extension to reload itself from disk. Useful after an update.",
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		b.mu.Lock()
		if b.conn == nil {
			b.mu.Unlock()
			return mcp.NewToolResultError("Extension not connected"), nil
		}
		msg, _ := json.Marshal(map[string]any{"type": "reload"})
		_, err := b.conn.Write(append(msg, '\n'))
		b.mu.Unlock()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(`{"ok": true}`), nil
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
