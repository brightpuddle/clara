package fs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestWaitForChangeMatchesCreate(t *testing.T) {
	root := t.TempDir()

	done := make(chan changeEvent, 1)
	go func() {
		result, err := waitForChange(context.Background(), root, false, map[string]struct{}{
			"create": {},
		}, 5*time.Second)
		if err != nil {
			t.Errorf("waitForChange: %v", err)
			return
		}
		done <- result
	}()

	time.Sleep(200 * time.Millisecond)
	target := filepath.Join(root, "file.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	select {
	case result := <-done:
		if result.Status != "matched" || result.Event != "create" {
			t.Fatalf("unexpected result: %#v", result)
		}
		if result.Path != target {
			t.Fatalf("unexpected path: %s", result.Path)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for create result")
	}
}

func TestWaitForChangeMatchesRecursiveCreate(t *testing.T) {
	root := t.TempDir()
	subdir := filepath.Join(root, "nested")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	done := make(chan changeEvent, 1)
	go func() {
		result, err := waitForChange(context.Background(), root, true, map[string]struct{}{
			"create": {},
		}, 5*time.Second)
		if err != nil {
			t.Errorf("waitForChange recursive: %v", err)
			return
		}
		done <- result
	}()

	time.Sleep(200 * time.Millisecond)
	target := filepath.Join(subdir, "child.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write nested file: %v", err)
	}

	select {
	case result := <-done:
		if result.Event != "create" {
			t.Fatalf("unexpected recursive event: %#v", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for recursive create")
	}
}

func TestHandleWaitForChangeTimesOut(t *testing.T) {
	root := t.TempDir()
	result, err := handleWaitForChange(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]any{
				"path":            root,
				"timeout_seconds": 0.1,
			},
		},
	})
	if err != nil {
		t.Fatalf("handleWaitForChange: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %#v", result)
	}
	content, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("unexpected structured content: %#v", result.StructuredContent)
	}
	if content["status"] != "timed_out" {
		t.Fatalf("unexpected timeout status: %#v", content)
	}
}
