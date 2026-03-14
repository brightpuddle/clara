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

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Description is the human-readable summary shown in clara tool list.
const Description = "Built-in filesystem server: read, write, list, search, move, and delete files."

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

	return s
}

// Serve starts the filesystem MCP server on stdio and blocks until ctx is done.
func Serve(ctx context.Context) error {
	return server.ServeStdio(New())
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
		ModTime: info.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	return mcp.NewToolResultText(string(data)), nil
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
