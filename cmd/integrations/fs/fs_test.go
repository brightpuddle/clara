package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestReadFileDecode(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "test.json")
	if err := os.WriteFile(path, []byte(`{"foo": "bar", "baz": 123}`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := handleReadFile(context.Background(), newCallRequest(map[string]any{
		"path":   path,
		"decode": "json",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("tool error: %s", result.Content[0].(mcp.TextContent).Text)
	}
	data := result.StructuredContent.(map[string]any)
	if data["foo"] != "bar" || data["baz"] != float64(123) {
		t.Fatalf("unexpected data: %#v", data)
	}

	// Test YAML
	pathYaml := filepath.Join(root, "test.yaml")
	if err := os.WriteFile(pathYaml, []byte("foo: bar\nbaz: 123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err = handleReadFile(context.Background(), newCallRequest(map[string]any{
		"path":   pathYaml,
		"decode": "yaml",
	}))
	if err != nil {
		t.Fatal(err)
	}
	data = result.StructuredContent.(map[string]any)
	if data["foo"] != "bar" || data["baz"] != 123 {
		t.Fatalf("unexpected data: %#v", data)
	}
}

func TestWriteFileEncode(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "test.json")

	data := map[string]any{
		"hello": "world",
		"nums":  []any{1, 2, 3},
	}

	_, err := handleWriteFile(context.Background(), newCallRequest(map[string]any{
		"path":   path,
		"data":   data,
		"encode": "json",
	}))
	if err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(content, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["hello"] != "world" {
		t.Fatalf("unexpected content: %s", string(content))
	}

	// Test YAML
	pathYaml := filepath.Join(root, "test.yaml")
	_, err = handleWriteFile(context.Background(), newCallRequest(map[string]any{
		"path":   pathYaml,
		"data":   data,
		"encode": "yaml",
	}))
	if err != nil {
		t.Fatal(err)
	}
	content, _ = os.ReadFile(pathYaml)
	if !strings.Contains(string(content), "hello: world") {
		t.Fatalf("unexpected yaml content: %s", string(content))
	}
}

func TestListDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file1.txt"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "subdir/file2.txt"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Non-recursive listing
	result, err := handleListDirectory(context.Background(), newCallRequest(map[string]any{
		"path":      root,
		"recursive": false,
	}))
	if err != nil {
		t.Fatal(err)
	}
	data := result.StructuredContent.([]directoryEntry)
	if len(data) != 2 {
		t.Fatalf("expected 2 items, got %d", len(data))
	}

	// Recursive listing
	result, err = handleListDirectory(context.Background(), newCallRequest(map[string]any{
		"path":      root,
		"recursive": true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	data = result.StructuredContent.([]directoryEntry)
	if len(data) != 3 { // subdir, file1.txt, subdir/file2.txt
		t.Fatalf("expected 3 items, got %d", len(data))
	}

	foundFile2 := false
	for _, item := range data {
		if item.Name == "subdir/file2.txt" {
			foundFile2 = true
			break
		}
	}
	if !foundFile2 {
		t.Error("expected subdir/file2.txt in recursive listing")
	}

	// Test symlink (should not follow)
	linkPath := filepath.Join(root, "link_to_subdir")
	if err := os.Symlink("subdir", linkPath); err != nil {
		t.Fatal(err)
	}

	result, err = handleListDirectory(context.Background(), newCallRequest(map[string]any{
		"path":      root,
		"recursive": false,
	}))
	if err != nil {
		t.Fatal(err)
	}
	data = result.StructuredContent.([]directoryEntry)
	foundLink := false
	for _, item := range data {
		if item.Name == "link_to_subdir" {
			foundLink = true
			if item.IsDir {
				t.Error("link_to_subdir should not be reported as Dir by WalkDir (it is a symlink)")
			}
		}
	}
	if !foundLink {
		t.Error("expected link_to_subdir in listing")
	}
}

func newCallRequest(args map[string]any) mcp.CallToolRequest {
	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: args,
		},
	}
}
