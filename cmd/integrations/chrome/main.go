package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/brightpuddle/clara"
	"github.com/brightpuddle/clara/pkg/contract"
	"github.com/cockroachdb/errors"
	"github.com/google/uuid"
	"github.com/hashicorp/go-plugin"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"
)

const (
	commandTimeout    = 5 * time.Minute
	heartbeatInterval = 15 * time.Second
)

type commandResult struct {
	Result json.RawMessage
	Error  string
}

type bridgeResponse struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Version string          `json:"version"`
	Result  json.RawMessage `json:"result"`
	Error   string          `json:"error"`
}

type Chrome struct {
	mu             sync.Mutex
	conn           net.Conn
	pending        map[string]chan commandResult
	log            zerolog.Logger
	currentVersion string
	ctx            context.Context
	cancel         context.CancelFunc
}

func NewChrome() *Chrome {
	ctx, cancel := context.WithCancel(context.Background())
	ver := embeddedExtensionVersion()
	c := &Chrome{
		pending:        make(map[string]chan commandResult),
		log:            zerolog.New(os.Stderr).With().Timestamp().Logger(),
		currentVersion: ver,
		ctx:            ctx,
		cancel:         cancel,
	}
	return c
}

func (c *Chrome) Configure(config []byte) error {
	// Start the bridge in the background if not already running.
	// In a real scenario, we might want to restart it if config changes,
	// but for now we just ensure it's running.
	go func() {
		if err := c.serveBridge(c.ctx); err != nil {
			c.log.Error().Err(err).Msg("Bridge server failed")
		}
	}()
	return nil
}

func (c *Chrome) Description() (string, error) {
	return "Built-in Chrome browser automation: navigate, click, fill, " +
		"upload files, read page content, screenshot, and manage tabs.", nil
}

func (c *Chrome) Tools() ([]byte, error) {
	tools := []mcp.Tool{
		mcp.NewTool(
			"navigate",
			mcp.WithDescription(
				"Navigate to a URL. Opens a new background tab by default. Returns the tab_id and final URL.",
			),
			mcp.WithString("url", mcp.Required(), mcp.Description("URL to navigate to.")),
			mcp.WithNumber(
				"tab_id",
				mcp.Description("Existing tab ID to navigate. If omitted, a new tab is opened."),
			),
			mcp.WithBoolean(
				"background",
				mcp.Description(
					"When true (default), the new tab is opened in the background without stealing focus.",
				),
			),
		),
		mcp.NewTool(
			"click",
			mcp.WithDescription(
				"Click an element identified by a CSS selector. Scrolls the element into view and dispatches a full click sequence.",
			),
			mcp.WithNumber(
				"tab_id",
				mcp.Required(),
				mcp.Description("ID of the tab that contains the element."),
			),
			mcp.WithString(
				"selector",
				mcp.Required(),
				mcp.Description("CSS selector for the element to click."),
			),
			mcp.WithNumber(
				"wait_after_ms",
				mcp.Description("Milliseconds to wait after clicking (default 500)."),
			),
		),
		mcp.NewTool(
			"fill",
			mcp.WithDescription("Fill a text input or textarea identified by a CSS selector."),
			mcp.WithNumber(
				"tab_id",
				mcp.Required(),
				mcp.Description("ID of the tab that contains the element."),
			),
			mcp.WithString(
				"selector",
				mcp.Required(),
				mcp.Description("CSS selector for the input element to fill."),
			),
			mcp.WithString("value", mcp.Required(), mcp.Description("Text value to set.")),
			mcp.WithBoolean(
				"clear_first",
				mcp.Description("Select all existing text before typing (default true)."),
			),
		),
		mcp.NewTool(
			"fill_by_label",
			mcp.WithDescription("Find a text input or textarea by its label text and fill it."),
			mcp.WithNumber(
				"tab_id",
				mcp.Required(),
				mcp.Description("ID of the tab that contains the element."),
			),
			mcp.WithString("label", mcp.Required(), mcp.Description("Label text to search for.")),
			mcp.WithString("value", mcp.Required(), mcp.Description("Text value to set.")),
			mcp.WithString(
				"tag",
				mcp.Description("HTML tag of the input element: 'input' (default) or 'textarea'."),
			),
		),
		mcp.NewTool(
			"click_by_label",
			mcp.WithDescription(
				"Find a button, link, or clickable element by its text and click it.",
			),
			mcp.WithNumber(
				"tab_id",
				mcp.Required(),
				mcp.Description("ID of the tab that contains the element."),
			),
			mcp.WithString(
				"label",
				mcp.Required(),
				mcp.Description("Text or aria-label to search for."),
			),
		),
		mcp.NewTool(
			"upload_file",
			mcp.WithDescription(
				"Set one or more local files on a <input type=\"file\"> element using the Chrome DevTools Protocol.",
			),
			mcp.WithNumber(
				"tab_id",
				mcp.Required(),
				mcp.Description("ID of the tab that contains the file input."),
			),
			mcp.WithString(
				"selector",
				mcp.Required(),
				mcp.Description("CSS selector for the <input type=\"file\"> element."),
			),
			mcp.WithString(
				"file_path",
				mcp.Description("Absolute path to one local file to upload."),
			),
			mcp.WithArray(
				"file_paths",
				mcp.Description("Optional array of absolute paths to upload in one selection."),
			),
		),
		mcp.NewTool(
			"eval",
			mcp.WithDescription(
				"Execute an async JavaScript snippet in the page context and return its JSON-serializable result.",
			),
			mcp.WithNumber(
				"tab_id",
				mcp.Required(),
				mcp.Description("ID of the tab where the script should run."),
			),
			mcp.WithString(
				"script",
				mcp.Required(),
				mcp.Description("JavaScript function body executed as async code."),
			),
			mcp.WithObject(
				"args",
				mcp.Description(
					"Optional JSON-serializable argument object passed into the script.",
				),
			),
		),
		mcp.NewTool(
			"screenshot",
			mcp.WithDescription(
				"Capture a PNG screenshot of the visible area of a tab. Returns a data URL.",
			),
			mcp.WithNumber(
				"tab_id",
				mcp.Description("ID of the tab to screenshot. If omitted, uses the active tab."),
			),
		),
		mcp.NewTool("read_page",
			mcp.WithDescription("Read the visible text content, title, and URL of a tab."),
			mcp.WithNumber("tab_id", mcp.Required(), mcp.Description("ID of the tab to read.")),
		),
		mcp.NewTool("get_tabs",
			mcp.WithDescription("List open browser tabs. Optionally filter by URL pattern."),
			mcp.WithString("url_filter", mcp.Description("Optional URL match pattern.")),
		),
		mcp.NewTool("close_tab",
			mcp.WithDescription("Close a browser tab by ID."),
			mcp.WithNumber("tab_id", mcp.Required(), mcp.Description("ID of the tab to close.")),
		),
		mcp.NewTool("cleanup_tabs",
			mcp.WithDescription("Close all browser tabs that were opened by Clara automation."),
		),
		mcp.NewTool(
			"wait_for_selector",
			mcp.WithDescription(
				"Wait until an element matching a CSS selector is present in the DOM.",
			),
			mcp.WithNumber(
				"tab_id",
				mcp.Required(),
				mcp.Description("ID of the tab to search in."),
			),
			mcp.WithString(
				"selector",
				mcp.Required(),
				mcp.Description("CSS selector to wait for."),
			),
			mcp.WithNumber(
				"timeout_seconds",
				mcp.Description("Maximum seconds to wait (default 30)."),
			),
		),
		mcp.NewTool(
			"wait_for_load",
			mcp.WithDescription("Wait until a tab's document status is 'complete'."),
			mcp.WithNumber("tab_id", mcp.Required(), mcp.Description("ID of the tab to wait on.")),
			mcp.WithNumber(
				"timeout_seconds",
				mcp.Description("Maximum seconds to wait (default 30)."),
			),
		),
		mcp.NewTool(
			"query_elements",
			mcp.WithDescription(
				"Query multiple elements by CSS selector and return their attributes.",
			),
			mcp.WithNumber(
				"tab_id",
				mcp.Required(),
				mcp.Description("ID of the tab to search in."),
			),
			mcp.WithString("selector", mcp.Required(), mcp.Description("CSS selector to query.")),
		),
		mcp.NewTool(
			"type",
			mcp.WithDescription(
				"Simulate character-by-character typing into the currently focused element.",
			),
			mcp.WithNumber("tab_id", mcp.Required(), mcp.Description("ID of the tab to type in.")),
			mcp.WithString("text", mcp.Required(), mcp.Description("Text to type.")),
			mcp.WithNumber(
				"delay_between_keys_ms",
				mcp.Description("Optional delay between keystrokes (default 10ms)."),
			),
		),
		mcp.NewTool(
			"debugger_command",
			mcp.WithDescription(
				"Directly execute a Chrome DevTools Protocol (CDP) command on a tab.",
			),
			mcp.WithNumber("tab_id", mcp.Required(), mcp.Description("ID of the tab to target.")),
			mcp.WithString("method", mcp.Required(), mcp.Description("CDP method name.")),
			mcp.WithObject("params", mcp.Description("Optional CDP parameters object.")),
		),
		mcp.NewTool(
			"type_by_selector",
			mcp.WithDescription(
				"Type text into an element identified by CSS selector using native CDP commands.",
			),
			mcp.WithNumber("tab_id", mcp.Required(), mcp.Description("ID of the tab to target.")),
			mcp.WithString(
				"selector",
				mcp.Required(),
				mcp.Description("CSS selector of the element to type into."),
			),
			mcp.WithString("text", mcp.Required(), mcp.Description("Text to type.")),
			mcp.WithNumber(
				"delay_between_keys_ms",
				mcp.Description("Optional delay between keystrokes (default 10ms)."),
			),
		),
		mcp.NewTool(
			"extension_status",
			mcp.WithDescription(
				"Check if the Clara Chrome extension is currently connected to the bridge.",
			),
		),
		mcp.NewTool("reload_extension",
			mcp.WithDescription("Signal the Chrome extension to reload itself from disk."),
		),
	}
	return json.Marshal(tools)
}

func (c *Chrome) CallTool(name string, args []byte) ([]byte, error) {
	var params map[string]any
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, errors.Wrap(err, "unmarshal args")
	}

	if name == "extension_status" {
		connected := c.isConnected()
		return json.Marshal(map[string]any{"connected": connected})
	}

	if name == "reload_extension" {
		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		if conn == nil {
			return nil, errors.New("extension not connected")
		}
		msg, _ := json.Marshal(map[string]any{"type": "reload"})
		_, err := conn.Write(append(msg, '\n'))
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"ok": true})
	}

	raw, err := c.execute(c.ctx, name, params)
	if err != nil {
		return nil, err
	}
	if raw == nil || string(raw) == "null" {
		return json.Marshal(map[string]any{"ok": true})
	}
	return raw, nil
}

func (c *Chrome) isConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil
}

func (c *Chrome) execute(
	ctx context.Context,
	tool string,
	params map[string]any,
) (json.RawMessage, error) {
	c.mu.Lock()
	conn := c.conn
	if conn == nil {
		c.mu.Unlock()
		return nil, errors.New("Chrome extension not connected")
	}

	id := uuid.New().String()
	ch := make(chan commandResult, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	cmd := map[string]any{
		"id":     id,
		"tool":   tool,
		"params": params,
	}

	raw, err := json.Marshal(cmd)
	if err != nil {
		return nil, err
	}

	if _, err := conn.Write(append(raw, '\n')); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(commandTimeout):
		return nil, fmt.Errorf("command %q timed out", tool)
	case res := <-ch:
		if res.Error != "" {
			return nil, errors.New(res.Error)
		}
		return res.Result, nil
	}
}

func (c *Chrome) serveBridge(ctx context.Context) error {
	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".local", "share", "clara")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return err
	}
	udsPath := filepath.Join(dataDir, "chrome-bridge.sock")
	_ = os.Remove(udsPath)

	udsLn, err := net.Listen("unix", udsPath)
	if err != nil {
		return errors.Wrap(err, "listen unix")
	}
	defer udsLn.Close()

	go func() {
		<-ctx.Done()
		udsLn.Close()
	}()

	errCh := make(chan error, 1)
	go func() {
		for {
			conn, err := udsLn.Accept()
			if err != nil {
				if ctx.Err() == nil {
					errCh <- err
				}
				return
			}
			go c.handleConn(conn)
		}
	}()

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			c.mu.Lock()
			conn := c.conn
			c.mu.Unlock()
			if conn != nil {
				msg, _ := json.Marshal(map[string]any{"type": "ping"})
				if _, err := conn.Write(append(msg, '\n')); err != nil {
					c.mu.Lock()
					if c.conn == conn {
						c.conn = nil
					}
					c.mu.Unlock()
					conn.Close()
				}
			}
		case err := <-errCh:
			return err
		}
	}
}

func (c *Chrome) handleConn(conn net.Conn) {
	c.mu.Lock()
	old := c.conn
	c.conn = conn
	c.mu.Unlock()

	if old != nil {
		_ = old.Close()
	}

	decoder := json.NewDecoder(conn)
	for {
		var resp bridgeResponse
		if err := decoder.Decode(&resp); err != nil {
			break
		}
		c.dispatch(resp, conn)
	}
}

func (c *Chrome) dispatch(resp bridgeResponse, conn net.Conn) {
	if resp.Type == "pong" {
		return
	}
	if resp.Type == "ping" {
		msg, _ := json.Marshal(map[string]any{"type": "pong"})
		_, _ = conn.Write(append(msg, '\n'))
		return
	}

	if resp.Type == "hello" {
		if resp.Version != c.currentVersion {
			if err := updateExtensionFiles(); err == nil {
				msg, _ := json.Marshal(map[string]any{"type": "update"})
				_, _ = conn.Write(append(msg, '\n'))
			}
		}
		return
	}

	if resp.ID == "" {
		return
	}

	c.mu.Lock()
	ch, ok := c.pending[resp.ID]
	c.mu.Unlock()

	if ok {
		select {
		case ch <- commandResult{Result: resp.Result, Error: resp.Error}:
		default:
		}
	}
}

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

func updateExtensionFiles() error {
	home, _ := os.UserHomeDir()
	target := filepath.Join(home, ".local", "share", "clara", "extension")
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

func main() {
	chrome := NewChrome()

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: contract.HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"chrome": &contract.ChromeIntegrationPlugin{Impl: chrome},
		},
	})
}
