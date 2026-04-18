package fs

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
func TestWriteFileMarkdown(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "test.md")
	initialContent := `---
title: Test Note
tags: [a, b]
---
# Hello
Body here.
`
	if err := os.WriteFile(path, []byte(initialContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. Read and decode
	result, err := handleReadFile(context.Background(), newCallRequest(map[string]any{
		"path":   path,
		"decode": "markdown",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("read error: %v", result.Content[0])
	}

	// In real usage, this would be JSON-ified to a map. 
	// Since we are calling the handler directly, we get the struct.
	// We convert it to map to simulate real tool call arguments.
	jsonData, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal(jsonData, &data); err != nil {
		t.Fatal(err)
	}
	
	// 2. Modify
	data["content"] = "# Modified\nNew body."
	fm := data["frontmatter"].(map[string]any)
	fm["title"] = "Modified Title"

	// 3. Write and encode
	newPath := filepath.Join(root, "modified.md")
	result, err = handleWriteFile(context.Background(), newCallRequest(map[string]any{
		"path":   newPath,
		"data":   data,
		"encode": "markdown",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("write error: %v", result.Content[0])
	}
	// 4. Verify content
	content, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatal(err)
	}
	s := string(content)
	if !strings.Contains(s, "title: Modified Title") {
		t.Errorf("missing title in output:\n%s", s)
	}
	if !strings.Contains(s, "# Modified") {
		t.Errorf("missing content in output:\n%s", s)
	}
	if !strings.HasPrefix(s, "---") {
		t.Errorf("missing frontmatter delimiter:\n%s", s)
	}
}
