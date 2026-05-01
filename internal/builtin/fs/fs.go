// Package fs provides the built-in filesystem integration as an in-process tool.
// It registers read, write, list, search, move, delete, and watch tools.
package fs

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

	"github.com/brightpuddle/clara/internal/builtin/pathutil"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/cockroachdb/errors"
	"github.com/fsnotify/fsnotify"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"
	"gopkg.in/yaml.v3"
)

const description = "Built-in filesystem: read, write, list, search, move, delete, watch."

// server holds file-watch subscriptions and a reference to the registry for
// emitting on_change notifications.
type server struct {
	ctx     context.Context
	reg     *registry.Registry
	watchMu sync.Mutex
	watches map[string]context.CancelFunc
}

// Register adds all fs tools into reg under the "fs" namespace.
// The context is used to bound all background watch goroutines.
func Register(
	ctx context.Context,
	_ map[string]any,
	reg *registry.Registry,
	log zerolog.Logger,
) error {
	log.Debug().Msg("registering fs builtin")

	s := &server{
		ctx:     ctx,
		reg:     reg,
		watches: make(map[string]context.CancelFunc),
	}

	reg.RegisterNamespaceDescription("fs", description)

	tools := []struct {
		spec    mcp.Tool
		handler func(context.Context, map[string]any) (any, error)
	}{
		{
			mcp.NewTool("fs.clara_list_events",
				mcp.WithDescription("List the available event triggers emitted by this server."),
			),
			func(_ context.Context, _ map[string]any) (any, error) {
				return []map[string]any{
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
				}, nil
			},
		},
		{
			mcp.NewTool("fs.watch_subscribe",
				mcp.WithDescription(
					"Start watching a file or directory for changes. "+
						"Returns a subscription_id that must be passed to watch_unsubscribe to stop watching. "+
						"Emits fs/on_change notifications when changes are detected.",
				),
				mcp.WithString("path",
					mcp.Required(),
					mcp.Description("File or directory to watch. Supports ~ and environment variables."),
				),
				mcp.WithBoolean("recursive",
					mcp.Description("Whether to recursively watch subdirectories. Defaults to false."),
				),
			),
			s.handleWatchSubscribe,
		},
		{
			mcp.NewTool("fs.watch_unsubscribe",
				mcp.WithDescription("Stop a previously registered file watch."),
				mcp.WithString("subscription_id",
					mcp.Required(),
					mcp.Description("The subscription_id returned by watch_subscribe."),
				),
			),
			s.handleWatchUnsubscribe,
		},
		{
			mcp.NewTool("fs.read_file",
				mcp.WithDescription("Read the complete contents of a file from the filesystem."),
				mcp.WithString("path",
					mcp.Required(),
					mcp.Description("Absolute or relative path to the file to read."),
				),
				mcp.WithString("decode",
					mcp.Description(
						"Optional decoder: json, yaml. When set, returns parsed data instead of raw text.",
					),
				),
			),
			handleReadFile,
		},
		{
			mcp.NewTool("fs.write_file",
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
			),
			handleWriteFile,
		},
		{
			mcp.NewTool("fs.list_directory",
				mcp.WithDescription("List the contents of a directory."),
				mcp.WithString("path",
					mcp.Required(),
					mcp.Description("Absolute or relative path to the directory to list."),
				),
				mcp.WithBoolean("recursive",
					mcp.Description("Whether to recursively list subdirectories. Defaults to false."),
				),
			),
			handleListDirectory,
		},
		{
			mcp.NewTool("fs.search_files",
				mcp.WithDescription("Search for files matching a glob pattern under a directory."),
				mcp.WithString("root",
					mcp.Required(),
					mcp.Description("Root directory to search in."),
				),
				mcp.WithString("pattern",
					mcp.Required(),
					mcp.Description("Glob pattern to match file names against (e.g. '*.go')."),
				),
			),
			handleSearchFiles,
		},
		{
			mcp.NewTool("fs.move_file",
				mcp.WithDescription("Move or rename a file or directory."),
				mcp.WithString("source",
					mcp.Required(),
					mcp.Description("Source path."),
				),
				mcp.WithString("destination",
					mcp.Required(),
					mcp.Description("Destination path."),
				),
			),
			handleMoveFile,
		},
		{
			mcp.NewTool("fs.delete_file",
				mcp.WithDescription("Delete a file or empty directory."),
				mcp.WithString("path",
					mcp.Required(),
					mcp.Description("Path to the file or empty directory to delete."),
				),
			),
			handleDeleteFile,
		},
		{
			mcp.NewTool("fs.create_directory",
				mcp.WithDescription("Create a directory and all necessary parent directories."),
				mcp.WithString("path",
					mcp.Required(),
					mcp.Description("Path of the directory to create."),
				),
			),
			handleCreateDirectory,
		},
		{
			mcp.NewTool("fs.get_file_info",
				mcp.WithDescription(
					"Get metadata about a file or directory (size, modification time, type).",
				),
				mcp.WithString("path",
					mcp.Required(),
					mcp.Description("Path to the file or directory."),
				),
			),
			handleGetFileInfo,
		},
		{
			mcp.NewTool("fs.path_exists",
				mcp.WithDescription("Return whether a file or directory exists at the given path."),
				mcp.WithString("path",
					mcp.Required(),
					mcp.Description("Path to check."),
				),
			),
			handlePathExists,
		},
		{
			mcp.NewTool("fs.search",
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
			),
			s.handleMdfindSearch,
		},
	}

	for _, t := range tools {
		reg.RegisterWithSpec(t.spec, t.handler)
	}

	return nil
}

// ── Watch handlers ────────────────────────────────────────────────────────────

func (s *server) handleWatchSubscribe(
	_ context.Context,
	args map[string]any,
) (any, error) {
	path, err := stringArg(args, "path")
	if err != nil {
		return nil, err
	}
	recursive := boolArg(args, "recursive")

	var idBytes [8]byte
	if _, err := rand.Read(idBytes[:]); err != nil {
		return nil, errors.New("watch_subscribe: failed to generate subscription ID")
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
		s.watchAndNotify(watchCtx, path, recursive)
	}()

	return map[string]any{"subscription_id": subID}, nil
}

func (s *server) handleWatchUnsubscribe(
	_ context.Context,
	args map[string]any,
) (any, error) {
	subID, err := stringArg(args, "subscription_id")
	if err != nil {
		return nil, err
	}

	s.watchMu.Lock()
	cancel, ok := s.watches[subID]
	if ok {
		delete(s.watches, subID)
	}
	s.watchMu.Unlock()

	if !ok {
		return nil, errors.Newf("watch_unsubscribe: subscription %q not found", subID)
	}
	cancel()
	return map[string]any{"status": "removed"}, nil
}

func (s *server) watchAndNotify(ctx context.Context, root string, recursive bool) {
	root = pathutil.Resolve(root)
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

			s.reg.EmitNotification("fs", "on_change", map[string]any{
				"status":    "matched",
				"path":      event.Name,
				"root":      absRoot,
				"event":     changeType,
				"recursive": recursive,
				"timestamp": time.Now().UTC().Format(time.RFC3339),
			})
		}
	}
}

// ── Tool handlers ─────────────────────────────────────────────────────────────

func handleReadFile(_ context.Context, args map[string]any) (any, error) {
	path, err := stringArg(args, "path")
	if err != nil {
		return nil, err
	}
	decode := stringArgOptional(args, "decode")

	path = pathutil.Resolve(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "read_file")
	}

	if decode != "" {
		var parsed any
		switch strings.ToLower(decode) {
		case "json":
			if err := json.Unmarshal(data, &parsed); err != nil {
				return nil, errors.Wrapf(err, "read_file json decode")
			}
		case "yaml":
			if err := yaml.Unmarshal(data, &parsed); err != nil {
				return nil, errors.Wrapf(err, "read_file yaml decode")
			}
		default:
			return nil, errors.Newf("read_file: unsupported decode format %q", decode)
		}
		return parsed, nil
	}

	return string(data), nil
}

func handleWriteFile(_ context.Context, args map[string]any) (any, error) {
	path, err := stringArg(args, "path")
	if err != nil {
		return nil, err
	}

	var content []byte
	encode := stringArgOptional(args, "encode")
	data, hasData := args["data"]

	if hasData {
		if encode == "" {
			return nil, errors.New("write_file: 'encode' must be set when 'data' is provided")
		}
		switch strings.ToLower(encode) {
		case "json":
			content, err = json.MarshalIndent(data, "", "  ")
		case "yaml":
			content, err = yaml.Marshal(data)
		default:
			return nil, errors.Newf("write_file: unsupported encode format %q", encode)
		}
		if err != nil {
			return nil, errors.Wrapf(err, "write_file encode")
		}
	} else {
		rawContent, err := stringArg(args, "content")
		if err != nil {
			return nil, err
		}
		content = []byte(rawContent)
	}

	path = pathutil.Resolve(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, errors.Wrapf(err, "write_file mkdir")
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return nil, errors.Wrapf(err, "write_file")
	}
	return map[string]any{"status": "success", "message": "file written successfully"}, nil
}

// directoryEntry is returned by list_directory.
type directoryEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size,omitempty"`
}

func handleListDirectory(_ context.Context, args map[string]any) (any, error) {
	path, err := stringArg(args, "path")
	if err != nil {
		return nil, err
	}
	recursive := boolArg(args, "recursive")
	path = pathutil.Resolve(path)

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
			rel, relErr := filepath.Rel(path, p)
			if relErr != nil {
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
		return nil, errors.Wrapf(err, "list_directory")
	}
	return result, nil
}

func handleSearchFiles(_ context.Context, args map[string]any) (any, error) {
	root, err := stringArg(args, "root")
	if err != nil {
		return nil, err
	}
	root = pathutil.Resolve(root)
	pattern, err := stringArg(args, "pattern")
	if err != nil {
		return nil, err
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
		return nil, errors.Wrapf(err, "search_files")
	}
	return matches, nil
}

func handleMoveFile(_ context.Context, args map[string]any) (any, error) {
	src, err := stringArg(args, "source")
	if err != nil {
		return nil, err
	}
	src = pathutil.Resolve(src)
	dst, err := stringArg(args, "destination")
	if err != nil {
		return nil, err
	}
	dst = pathutil.Resolve(dst)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return nil, errors.Wrapf(err, "move_file mkdir")
	}
	if err := os.Rename(src, dst); err != nil {
		return nil, errors.Wrapf(err, "move_file")
	}
	return map[string]any{"status": "success", "message": "file moved successfully"}, nil
}

func handleDeleteFile(_ context.Context, args map[string]any) (any, error) {
	path, err := stringArg(args, "path")
	if err != nil {
		return nil, err
	}
	path = pathutil.Resolve(path)
	if err := os.Remove(path); err != nil {
		return nil, errors.Wrapf(err, "delete_file")
	}
	return map[string]any{"status": "success", "message": "deleted successfully"}, nil
}

func handleCreateDirectory(_ context.Context, args map[string]any) (any, error) {
	path, err := stringArg(args, "path")
	if err != nil {
		return nil, err
	}
	path = pathutil.Resolve(path)
	if err := os.MkdirAll(path, 0o755); err != nil {
		return nil, errors.Wrapf(err, "create_directory")
	}
	return map[string]any{"status": "success", "message": "directory created"}, nil
}

func handleGetFileInfo(_ context.Context, args map[string]any) (any, error) {
	path, err := stringArg(args, "path")
	if err != nil {
		return nil, err
	}
	path = pathutil.Resolve(path)
	info, err := os.Stat(path)
	if err != nil {
		return nil, errors.Wrapf(err, "get_file_info")
	}
	return map[string]any{
		"name":     info.Name(),
		"size":     info.Size(),
		"is_dir":   info.IsDir(),
		"mode":     info.Mode().String(),
		"mod_time": info.ModTime().UTC().Format(time.RFC3339),
	}, nil
}

func handlePathExists(_ context.Context, args map[string]any) (any, error) {
	path, err := stringArg(args, "path")
	if err != nil {
		return nil, err
	}
	path = pathutil.Resolve(path)
	_, err = os.Stat(path)
	if err == nil {
		return map[string]any{"exists": true, "path": path}, nil
	}
	if os.IsNotExist(err) {
		return map[string]any{"exists": false, "path": path}, nil
	}
	return nil, errors.Wrapf(err, "path_exists")
}

func (s *server) handleMdfindSearch(ctx context.Context, args map[string]any) (any, error) {
	query, err := stringArg(args, "query")
	if err != nil {
		return nil, err
	}
	path := stringArgOptional(args, "path")
	if path != "" {
		path = pathutil.Resolve(path)
	}
	limit := intArg(args, "limit")

	results, err := runMdfind(ctx, query, path)
	if err != nil {
		return nil, err
	}

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// ── Path helpers ──────────────────────────────────────────────────────────────

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

// ── Argument helpers ──────────────────────────────────────────────────────────

func stringArg(args map[string]any, name string) (string, error) {
	val, ok := args[name]
	if !ok {
		return "", fmt.Errorf("missing required argument: %q", name)
	}
	s, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string", name)
	}
	return s, nil
}

func stringArgOptional(args map[string]any, name string) string {
	val, ok := args[name]
	if !ok {
		return ""
	}
	s, ok := val.(string)
	if !ok {
		return ""
	}
	return s
}

func boolArg(args map[string]any, name string) bool {
	raw, ok := args[name]
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

func intArg(args map[string]any, name string) int {
	val, ok := args[name]
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
