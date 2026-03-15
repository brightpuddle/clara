// Package fs provides a built-in MCP filesystem server for Clara.
// It exposes file system operations as MCP tools over stdio.
package fs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/fsnotify/fsnotify"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Description is the human-readable summary shown in clara tool list.
const Description = "Built-in filesystem server: read, write, list, search, move, delete, and wait on folder changes."

// New creates a configured MCP server with all filesystem tools registered.
func New() *server.MCPServer {
	s := server.NewMCPServer(
		"clara-fs",
		"0.1.0",
		server.WithToolCapabilities(true),
	)

	s.AddTool(mcp.NewTool("read_file",
		mcp.WithDescription("Read the complete contents of a file from the filesystem."),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Absolute or relative path to the file to read."),
		),
	), handleReadFile)

	s.AddTool(mcp.NewTool(
		"write_file",
		mcp.WithDescription(
			"Write content to a file, creating it or overwriting it if it already exists.",
		),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Absolute or relative path to the file to write."),
		),
		mcp.WithString("content",
			mcp.Required(),
			mcp.Description("Content to write to the file."),
		),
	), handleWriteFile)

	s.AddTool(mcp.NewTool("list_directory",
		mcp.WithDescription("List the contents of a directory."),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Absolute or relative path to the directory to list."),
		),
	), handleListDirectory)

	s.AddTool(mcp.NewTool("search_files",
		mcp.WithDescription("Search for files matching a glob pattern under a directory."),
		mcp.WithString("root",
			mcp.Required(),
			mcp.Description("Root directory to search in."),
		),
		mcp.WithString("pattern",
			mcp.Required(),
			mcp.Description("Glob pattern to match file names against (e.g. '*.go')."),
		),
	), handleSearchFiles)

	s.AddTool(mcp.NewTool("move_file",
		mcp.WithDescription("Move or rename a file or directory."),
		mcp.WithString("source",
			mcp.Required(),
			mcp.Description("Source path."),
		),
		mcp.WithString("destination",
			mcp.Required(),
			mcp.Description("Destination path."),
		),
	), handleMoveFile)

	s.AddTool(mcp.NewTool("delete_file",
		mcp.WithDescription("Delete a file or empty directory."),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Path to the file or empty directory to delete."),
		),
	), handleDeleteFile)

	s.AddTool(mcp.NewTool("create_directory",
		mcp.WithDescription("Create a directory and all necessary parent directories."),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Path of the directory to create."),
		),
	), handleCreateDirectory)

	s.AddTool(mcp.NewTool(
		"get_file_info",
		mcp.WithDescription(
			"Get metadata about a file or directory (size, modification time, type).",
		),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Path to the file or directory."),
		),
	), handleGetFileInfo)

	s.AddTool(mcp.NewTool(
		"wait_for_change",
		mcp.WithDescription("Block until a matching create, change, or delete happens under a directory."),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Directory to watch for filesystem changes."),
		),
		mcp.WithArray("events",
			mcp.Description("Optional array of event kinds to match: create, change, delete."),
		),
		mcp.WithBoolean("recursive",
			mcp.Description("When true, watch existing and newly created subdirectories."),
		),
		mcp.WithNumber("timeout_seconds",
			mcp.Description("Optional timeout in seconds. When reached, the tool returns timed_out."),
		),
	), handleWaitForChange)

	return s
}

// Serve starts the filesystem MCP server on stdio and blocks until ctx is done.
func Serve(ctx context.Context) error {
	return server.ServeStdio(New())
}

type changeEvent struct {
	Status    string `json:"status"`
	Path      string `json:"path,omitempty"`
	Root      string `json:"root"`
	Event     string `json:"event,omitempty"`
	Recursive bool   `json:"recursive"`
	Timestamp string `json:"timestamp,omitempty"`
}

// ── Tool handlers ─────────────────────────────────────────────────────────────

func handleReadFile(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := stringArg(req, "path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("read_file: %v", err)), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

func handleWriteFile(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := stringArg(req, "path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	content, err := stringArg(req, "content")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("write_file mkdir: %v", err)), nil
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("write_file: %v", err)), nil
	}
	return mcp.NewToolResultText("file written successfully"), nil
}

func handleListDirectory(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := stringArg(req, "path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("list_directory: %v", err)), nil
	}
	type entry struct {
		Name  string `json:"name"`
		IsDir bool   `json:"is_dir"`
		Size  int64  `json:"size,omitempty"`
	}
	result := make([]entry, 0, len(entries))
	for _, e := range entries {
		info, _ := e.Info()
		var size int64
		if info != nil && !e.IsDir() {
			size = info.Size()
		}
		result = append(result, entry{Name: e.Name(), IsDir: e.IsDir(), Size: size})
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	return mcp.NewToolResultText(string(data)), nil
}

func handleSearchFiles(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	root, err := stringArg(req, "root")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	pattern, err := stringArg(req, "pattern")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	var matches []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if !d.IsDir() {
			matched, _ := filepath.Match(pattern, filepath.Base(path))
			if matched {
				matches = append(matches, path)
			}
		}
		return nil
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("search_files: %v", err)), nil
	}
	return mcp.NewToolResultText(strings.Join(matches, "\n")), nil
}

func handleMoveFile(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	src, err := stringArg(req, "source")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	dst, err := stringArg(req, "destination")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("move_file mkdir: %v", err)), nil
	}
	if err := os.Rename(src, dst); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("move_file: %v", err)), nil
	}
	return mcp.NewToolResultText("file moved successfully"), nil
}

func handleDeleteFile(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := stringArg(req, "path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := os.Remove(path); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("delete_file: %v", err)), nil
	}
	return mcp.NewToolResultText("deleted successfully"), nil
}

func handleCreateDirectory(
	_ context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	path, err := stringArg(req, "path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("create_directory: %v", err)), nil
	}
	return mcp.NewToolResultText("directory created"), nil
}

func handleGetFileInfo(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := stringArg(req, "path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("get_file_info: %v", err)), nil
	}
	type fileInfo struct {
		Name    string `json:"name"`
		Size    int64  `json:"size"`
		IsDir   bool   `json:"is_dir"`
		Mode    string `json:"mode"`
		ModTime string `json:"mod_time"`
	}
	result := fileInfo{
		Name:    info.Name(),
		Size:    info.Size(),
		IsDir:   info.IsDir(),
		Mode:    info.Mode().String(),
		ModTime: info.ModTime().UTC().Format(time.RFC3339),
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	return mcp.NewToolResultText(string(data)), nil
}

func handleWaitForChange(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	root, err := stringArg(req, "path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	info, err := os.Stat(root)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("wait_for_change: %v", err)), nil
	}
	if !info.IsDir() {
		return mcp.NewToolResultError("wait_for_change: path must be a directory"), nil
	}

	recursive := boolArg(req, "recursive")
	eventFilter, err := eventFilterArg(req, "events")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	timeout := durationSecondsArg(req, "timeout_seconds")

	result, err := waitForChange(ctx, root, recursive, eventFilter, timeout)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("wait_for_change: %v", err)), nil
	}
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"status":    result.Status,
		"path":      result.Path,
		"root":      result.Root,
		"event":     result.Event,
		"recursive": result.Recursive,
		"timestamp": result.Timestamp,
	}), nil
}

func waitForChange(
	ctx context.Context,
	root string,
	recursive bool,
	eventFilter map[string]struct{},
	timeout time.Duration,
) (changeEvent, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return changeEvent{}, errors.Wrap(err, "resolve absolute watch path")
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return changeEvent{}, errors.Wrap(err, "create filesystem watcher")
	}
	defer watcher.Close()

	if recursive {
		if err := addRecursiveWatches(watcher, root); err != nil {
			return changeEvent{}, err
		}
	} else if err := watcher.Add(root); err != nil {
		return changeEvent{}, errors.Wrap(err, "watch directory")
	}

	var timeoutCh <-chan time.Time
	var timer *time.Timer
	if timeout > 0 {
		timer = time.NewTimer(timeout)
		timeoutCh = timer.C
		defer timer.Stop()
	}

	for {
		select {
		case <-ctx.Done():
			return changeEvent{}, errors.Wrap(ctx.Err(), "wait cancelled")
		case <-timeoutCh:
			return changeEvent{
				Status:    "timed_out",
				Root:      root,
				Recursive: recursive,
			}, nil
		case err := <-watcher.Errors:
			if err == nil {
				continue
			}
			return changeEvent{}, errors.Wrap(err, "watch filesystem")
		case event := <-watcher.Events:
			if event.Name == "" {
				continue
			}
			if recursive && event.Has(fsnotify.Create) {
				if info, statErr := os.Stat(event.Name); statErr == nil && info.IsDir() {
					if err := addRecursiveWatches(watcher, event.Name); err != nil {
						return changeEvent{}, err
					}
				}
			}

			changeType, ok := classifyEvent(event)
			if !ok {
				continue
			}
			if _, ok := eventFilter[changeType]; !ok {
				continue
			}
			return changeEvent{
				Status:    "matched",
				Path:      event.Name,
				Root:      root,
				Event:     changeType,
				Recursive: recursive,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			}, nil
		}
	}
}

func addRecursiveWatches(watcher *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return errors.Wrap(err, "walk watched directory")
		}
		if !d.IsDir() {
			return nil
		}
		if addErr := watcher.Add(path); addErr != nil {
			return errors.Wrapf(addErr, "watch directory %s", path)
		}
		return nil
	})
}

func classifyEvent(event fsnotify.Event) (string, bool) {
	switch {
	case event.Has(fsnotify.Create):
		return "create", true
	case event.Has(fsnotify.Write), event.Has(fsnotify.Chmod), event.Has(fsnotify.Rename):
		return "change", true
	case event.Has(fsnotify.Remove):
		return "delete", true
	default:
		return "", false
	}
}

func eventFilterArg(req mcp.CallToolRequest, name string) (map[string]struct{}, error) {
	if _, ok := req.GetArguments()[name]; !ok {
		return map[string]struct{}{
			"create": {},
			"change": {},
			"delete": {},
		}, nil
	}

	raw, ok := req.GetArguments()[name].([]any)
	if !ok || len(raw) == 0 {
		return nil, fmt.Errorf("argument %q must be a non-empty array of strings", name)
	}

	filter := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		value, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("argument %q must contain only strings", name)
		}
		value = strings.ToLower(strings.TrimSpace(value))
		switch value {
		case "create", "change", "delete":
			filter[value] = struct{}{}
		default:
			return nil, fmt.Errorf("argument %q contains unsupported event %q", name, value)
		}
	}
	return filter, nil
}

func boolArg(req mcp.CallToolRequest, name string) bool {
	raw, ok := req.GetArguments()[name]
	if !ok {
		return false
	}
	switch value := raw.(type) {
	case bool:
		return value
	case float64:
		return value != 0
	default:
		return false
	}
}

func durationSecondsArg(req mcp.CallToolRequest, name string) time.Duration {
	raw, ok := req.GetArguments()[name]
	if !ok {
		return 0
	}
	switch value := raw.(type) {
	case float64:
		if value <= 0 {
			return 0
		}
		return time.Duration(value * float64(time.Second))
	case int:
		if value <= 0 {
			return 0
		}
		return time.Duration(value) * time.Second
	default:
		return 0
	}
}

// stringArg extracts a required string argument from a tool call request.
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
