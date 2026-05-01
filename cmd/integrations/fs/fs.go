package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/fsnotify/fsnotify"
	"github.com/mark3labs/mcp-go/mcp"
	"gopkg.in/yaml.v3"
)

// Description is the human-readable summary shown in clara tool list.
const Description = "Built-in filesystem server: read, write, list, search, move, and delete files."

// ToolHandler represents a tool and its execution handler.
type ToolHandler struct {
	Tool    mcp.Tool
	Handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
}

// Server wraps the filesystem tools and manages watches.
type Server struct {
	tools   map[string]ToolHandler
	ctx     context.Context
	watchMu sync.Mutex
	watches map[string]context.CancelFunc
}

// New creates a configured Server with all filesystem tools registered.
func New(ctx context.Context) *Server {
	s := &Server{
		ctx:     ctx,
		watches: make(map[string]context.CancelFunc),
		tools:   make(map[string]ToolHandler),
	}

	s.addTool(mcp.NewTool("clara_list_events",
		mcp.WithDescription("List the available event triggers emitted by this server."),
	), handleListEvents)

	s.addTool(mcp.NewTool("watch_subscribe",
		mcp.WithDescription(
			"Start watching a file or directory for changes. "+
				"Returns a subscription_id that must be passed to watch_unsubscribe to stop watching. "+
				"Emits clara/on_change notifications when changes are detected.",
		),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("File or directory to watch. Supports ~ and environment variables."),
		),
		mcp.WithBoolean("recursive",
			mcp.Description("Whether to recursively watch subdirectories. Defaults to false."),
		),
	), s.handleWatchSubscribe)

	s.addTool(mcp.NewTool("watch_unsubscribe",
		mcp.WithDescription("Stop a previously registered file watch."),
		mcp.WithString("subscription_id",
			mcp.Required(),
			mcp.Description("The subscription_id returned by watch_subscribe."),
		),
	), s.handleWatchUnsubscribe)

	s.addTool(mcp.NewTool("read_file",
		mcp.WithDescription("Read the complete contents of a file from the filesystem."),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Absolute or relative path to the file to read."),
		),
		mcp.WithString(
			"decode",
			mcp.Description(
				"Optional decoder: json, yaml. When set, returns parsed data instead of raw text.",
			),
		),
	), handleReadFile)

	s.addTool(mcp.NewTool(
		"write_file",
		mcp.WithDescription(
			"Write content to a file, creating it or overwriting it if it already exists.",
		),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Absolute or relative path to the file to write."),
		),
		mcp.WithString("content",
			mcp.Description("Content to write to the file."),
		),
		mcp.WithAny("data",
			mcp.Description("Structured data to write. When set, encode must also be set."),
		),
		mcp.WithString("encode",
			mcp.Description("Optional encoder: json, yaml."),
		),
	), handleWriteFile)

	s.addTool(mcp.NewTool("list_directory",
		mcp.WithDescription("List the contents of a directory."),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Absolute or relative path to the directory to list."),
		),
		mcp.WithBoolean("recursive",
			mcp.Description("Whether to recursively list subdirectories. Defaults to false."),
		),
	), handleListDirectory)

	s.addTool(mcp.NewTool("search_files",
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

	s.addTool(mcp.NewTool("move_file",
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

	s.addTool(mcp.NewTool("delete_file",
		mcp.WithDescription("Delete a file or empty directory."),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Path to the file or empty directory to delete."),
		),
	), handleDeleteFile)

	s.addTool(mcp.NewTool("create_directory",
		mcp.WithDescription("Create a directory and all necessary parent directories."),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Path of the directory to create."),
		),
	), handleCreateDirectory)

	s.addTool(mcp.NewTool(
		"get_file_info",
		mcp.WithDescription(
			"Get metadata about a file or directory (size, modification time, type).",
		),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Path to the file or directory."),
		),
	), handleGetFileInfo)

	s.addTool(mcp.NewTool(
		"path_exists",
		mcp.WithDescription("Return whether a file or directory exists at the given path."),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Path to check."),
		),
	), handlePathExists)

	s.addTool(mcp.NewTool("fs.search",
		mcp.WithDescription("Search for files using macOS mdfind (Spotlight)."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("The mdfind query string (e.g. 'kMDItemDisplayName == \"*.go\"')."),
		),
		mcp.WithString("path",
			mcp.Description("Optional directory to limit the search scope to."),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of results to return."),
		),
	), s.handleMdfindSearch)

	return s
}

func (s *Server) addTool(
	tool mcp.Tool,
	handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error),
) {
	s.tools[tool.Name] = ToolHandler{
		Tool:    tool,
		Handler: handler,
	}
}

func (s *Server) ListTools() map[string]ToolHandler {
	return s.tools
}

func (s *Server) handleMdfindSearch(
	ctx context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	query, err := stringArg(req, "query")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	path := stringArgOptional(req, "path")
	if path != "" {
		path = resolvePath(path)
	}
	limit := intArg(req, "limit")

	results, err := runMdfind(ctx, query, path)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	res, err := mcp.NewToolResultJSON(results)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal results: %v", err)), nil
	}
	return res, nil
}

func (s *Server) handleWatchSubscribe(
	_ context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	path, err := stringArg(req, "path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	recursive := boolArg(req, "recursive")

	var idBytes [8]byte
	if _, err := rand.Read(idBytes[:]); err != nil {
		return mcp.NewToolResultError("watch_subscribe: failed to generate subscription ID"), nil
	}
	subID := hex.EncodeToString(idBytes[:])

	watchCtx, cancel := context.WithCancel(s.ctx)
	s.watchMu.Lock()
	s.watches[subID] = cancel
	s.watchMu.Unlock()

	go func() {
		defer func() {
			s.watchMu.Lock()
			delete(s.watches, subID)
			s.watchMu.Unlock()
		}()
		watchAndNotify(watchCtx, path, recursive)
	}()

	return mcp.NewToolResultStructuredOnly(map[string]any{
		"subscription_id": subID,
	}), nil
}

func (s *Server) handleWatchUnsubscribe(
	_ context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	subID, err := stringArg(req, "subscription_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	s.watchMu.Lock()
	cancel, ok := s.watches[subID]
	if ok {
		delete(s.watches, subID)
	}
	s.watchMu.Unlock()

	if !ok {
		return mcp.NewToolResultError(
			fmt.Sprintf("watch_unsubscribe: subscription %q not found", subID),
		), nil
	}
	cancel()
	return mcp.NewToolResultStructuredOnly(map[string]any{"status": "removed"}), nil
}

func watchAndNotify(ctx context.Context, root string, recursive bool) {
	root = resolvePath(root)
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	defer watcher.Close()

	if recursive {
		_ = addRecursiveWatches(watcher, absRoot)
	} else {
		_ = watcher.Add(absRoot)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case err := <-watcher.Errors:
			if err == nil {
				continue
			}
		case event := <-watcher.Events:
			if event.Name == "" {
				continue
			}
			if recursive && event.Has(fsnotify.Create) {
				if info, statErr := os.Stat(event.Name); statErr == nil && info.IsDir() {
					_ = addRecursiveWatches(watcher, event.Name)
				}
			}

			changeType, ok := classifyEvent(event)
			if !ok {
				continue
			}

			notification := map[string]any{
				"jsonrpc": "2.0",
				"method":  "clara/on_change",
				"params": map[string]any{
					"status":    "matched",
					"path":      event.Name,
					"root":      absRoot,
					"event":     changeType,
					"recursive": recursive,
					"timestamp": time.Now().UTC().Format(time.RFC3339),
				},
			}
			data, _ := json.Marshal(notification)
			fmt.Fprintln(os.Stdout, string(data))
		}
	}
}

func handleListEvents(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultStructuredOnly([]map[string]any{
		{
			"name":        "on_change",
			"description": "Emitted when a file or directory is created, changed, or deleted under the watched root.",
			"params": []map[string]any{
				{
					"name":        "path",
					"type":        "string",
					"description": "Absolute path of the changed file or directory",
				},
				{
					"name":        "event",
					"type":        "string",
					"description": "Change type: create, write, remove, or rename",
				},
				{
					"name":        "root",
					"type":        "string",
					"description": "The watched root directory",
				},
				{
					"name":        "timestamp",
					"type":        "string",
					"description": "ISO-8601 timestamp of the event",
				},
			},
		},
	}), nil
}

// ── Tool handlers ─────────────────────────────────────────────────────────────

func handleReadFile(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := stringArg(req, "path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	decode := stringArgOptional(req, "decode")

	path = resolvePath(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("read_file: %v", err)), nil
	}

	if decode != "" {
		var parsed any
		switch strings.ToLower(decode) {
		case "json":
			if err := json.Unmarshal(data, &parsed); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("read_file json decode: %v", err)), nil
			}
		case "yaml":
			if err := yaml.Unmarshal(data, &parsed); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("read_file yaml decode: %v", err)), nil
			}
		default:
			return mcp.NewToolResultError(
				fmt.Sprintf("read_file: unsupported decode format %q", decode),
			), nil
		}
		return mcp.NewToolResultStructuredOnly(parsed), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

func handleWriteFile(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := stringArg(req, "path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var content []byte
	encode := stringArgOptional(req, "encode")
	data, hasData := req.GetArguments()["data"]

	if hasData {
		if encode == "" {
			return mcp.NewToolResultError(
				"write_file: 'encode' must be set when 'data' is provided",
			), nil
		}
		var err error
		switch strings.ToLower(encode) {
		case "json":
			content, err = json.MarshalIndent(data, "", "  ")
		case "yaml":
			content, err = yaml.Marshal(data)
		default:
			return mcp.NewToolResultError(
				fmt.Sprintf("write_file: unsupported encode format %q", encode),
			), nil
		}
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("write_file encode: %v", err)), nil
		}
	} else {
		rawContent, err := stringArg(req, "content")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		content = []byte(rawContent)
	}

	path = resolvePath(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("write_file mkdir: %v", err)), nil
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("write_file: %v", err)), nil
	}
	res, err := mcp.NewToolResultJSON(
		map[string]any{"status": "success", "message": "file written successfully"},
	)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal result: %v", err)), nil
	}
	return res, nil
}

type directoryEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size,omitempty"`
}

func handleListDirectory(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := stringArg(req, "path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	recursive := boolArg(req, "recursive")
	path = resolvePath(path)

	var result []directoryEntry
	err = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if p == path {
				return err
			}
			return nil // skip unreadable entries
		}
		if p == path {
			return nil
		}

		var name string
		if recursive {
			rel, err := filepath.Rel(path, p)
			if err != nil {
				name = p
			} else {
				name = rel
			}
		} else {
			name = d.Name()
		}

		info, _ := d.Info()
		var size int64
		if info != nil && !d.IsDir() {
			size = info.Size()
		}
		result = append(result, directoryEntry{Name: name, IsDir: d.IsDir(), Size: size})

		if d.IsDir() && !recursive {
			return filepath.SkipDir
		}
		return nil
	})

	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("list_directory: %v", err)), nil
	}

	res, err := mcp.NewToolResultJSON(result)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal results: %v", err)), nil
	}
	return res, nil
}

func handleSearchFiles(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	root, err := stringArg(req, "root")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	root = resolvePath(root)
	pattern, err := stringArg(req, "pattern")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	matches := []string{}
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
	res, err := mcp.NewToolResultJSON(matches)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal results: %v", err)), nil
	}
	return res, nil
}

func handleMoveFile(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	src, err := stringArg(req, "source")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	src = resolvePath(src)
	dst, err := stringArg(req, "destination")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	dst = resolvePath(dst)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("move_file mkdir: %v", err)), nil
	}
	if err := os.Rename(src, dst); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("move_file: %v", err)), nil
	}
	res, err := mcp.NewToolResultJSON(
		map[string]any{"status": "success", "message": "file moved successfully"},
	)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal result: %v", err)), nil
	}
	return res, nil
}

func handleDeleteFile(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := stringArg(req, "path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	path = resolvePath(path)
	if err := os.Remove(path); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("delete_file: %v", err)), nil
	}
	res, err := mcp.NewToolResultJSON(
		map[string]any{"status": "success", "message": "deleted successfully"},
	)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal result: %v", err)), nil
	}
	return res, nil
}

func handleCreateDirectory(
	_ context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	path, err := stringArg(req, "path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	path = resolvePath(path)
	if err := os.MkdirAll(path, 0o755); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("create_directory: %v", err)), nil
	}
	res, err := mcp.NewToolResultJSON(
		map[string]any{"status": "success", "message": "directory created"},
	)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal result: %v", err)), nil
	}
	return res, nil
}

func handleGetFileInfo(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := stringArg(req, "path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	path = resolvePath(path)
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
	res, err := mcp.NewToolResultJSON(result)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal result: %v", err)), nil
	}
	return res, nil
}

func handlePathExists(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := stringArg(req, "path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	path = resolvePath(path)
	_, err = os.Stat(path)
	if err == nil {
		return mcp.NewToolResultStructuredOnly(map[string]any{"exists": true, "path": path}), nil
	}
	if os.IsNotExist(err) {
		return mcp.NewToolResultStructuredOnly(map[string]any{"exists": false, "path": path}), nil
	}
	return mcp.NewToolResultError(fmt.Sprintf("path_exists: %v", err)), nil
}

func resolvePath(path string) string {
	path = os.ExpandEnv(strings.TrimSpace(path))
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
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

func stringArgOptional(req mcp.CallToolRequest, name string) string {
	val, ok := req.GetArguments()[name]
	if !ok {
		return ""
	}
	s, ok := val.(string)
	if !ok {
		return ""
	}
	return s
}
