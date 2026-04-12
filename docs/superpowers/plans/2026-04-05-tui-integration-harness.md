# TUI Integration Testing Harness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create a robust, Go-native integration testing framework for the TUI, Daemon, and Starlark components using in-memory MCP transports.

**Architecture:** A `TUIIntegrationHarness` orchestrates a Mock Registry, a Headless TUI (running the real MCP server logic), and a Starlark Interpreter connected via `net.Pipe()`.

**Tech Stack:** Go, Starlark, Bubble Tea (tea.Model), MCP (model-context-protocol).

---

### Task 1: Refactor TUI to expose MCP Server Factory

**Files:**
- Modify: `internal/tui/app.go`

- [ ] **Step 1: Extract `NewTUIServer` from `Run`**

Modify `internal/tui/app.go` to separate the MCP server construction from the terminal initialization.

```go
// NewTUIServer creates an MCPServer configured with the TUI's notification tools.
// It sends messages to the provided Bubble Tea program.
func NewTUIServer(p *tea.Program) *server.MCPServer {
	mcpSrv := server.NewMCPServer("clara_tui", "1.0.0")

	// ... move triageTool, notifyTool, interactiveTool and their AddTool calls here ...
    // ensure they use p.Send() to communicate with the model.

	return mcpSrv
}
```

- [ ] **Step 2: Update `Run` to use the new factory**

```go
func Run(cfg *config.Config) error {
    // ... NewProgram logic ...
    mcpSrv := NewTUIServer(p)
    // ... StartDynamicMCP and p.Run() ...
}
```

- [ ] **Step 3: Run existing tests to ensure no regressions**

Run: `go test ./internal/tui/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/tui/app.go
git commit -m "refactor: extract NewTUIServer factory for testing"
```

---

### Task 2: Create the `TUIIntegrationHarness`

**Files:**
- Create: `internal/tui/harness_test.go`

- [ ] **Step 1: Define the `TUIIntegrationHarness` struct**

```go
package tui

import (
	"context"
	"net"
	"testing"

	"github.com/brightpuddle/clara/internal/registry"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
)

type TUIIntegrationHarness struct {
	Registry   *registry.Registry
	TUIModel   *appModel
	TUIProgram *tea.Program
	TUIServer  *server.MCPServer
	ctx        context.Context
	cancel     context.CancelFunc
}

func NewHarness(t *testing.T) *TUIIntegrationHarness {
	ctx, cancel := context.WithCancel(context.Background())
	reg := registry.New(zerolog.Nop())
	theme := DefaultTheme()
	m := &appModel{
		theme:   theme,
		content: newContentModel(theme),
		prompt:  newPromptModel(theme),
	}
	p := tea.NewProgram(m)
	srv := NewTUIServer(p)

	h := &TUIIntegrationHarness{
		Registry:   reg,
		TUIModel:   m,
		TUIProgram: p,
		TUIServer:  srv,
		ctx:        ctx,
		cancel:     cancel,
	}

	// Connect using net.Pipe
	cConn, sConn := net.Pipe()
	
	// Start TUI Server
	go func() {
		s := server.NewStdioServer(srv)
		_ = s.Listen(ctx, sConn, sConn)
	}()

	// Connect Registry Client
	transport := registry.NewConnTransport(cConn)
	mcpClient := client.NewClient(transport)
	if err := mcpClient.Start(ctx); err != nil {
		t.Fatalf("failed to start mcp client: %v", err)
	}

	// Initialize and Register
	initRes, err := mcpClient.Initialize(ctx, mcp.InitializeRequest{})
	if err != nil {
		t.Fatalf("mcp initialize failed: %v", err)
	}

	toolsRes, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("mcp list tools failed: %v", err)
	}

	caps := &registry.ServerCapabilities{
		Name: "tui",
		Tools: toolsRes.Tools,
	}

	if err := reg.RegisterConnectedClient("tui", mcpClient, caps, transport.Close); err != nil {
		t.Fatalf("failed to register tui client: %v", err)
	}

	return h
}

func (h *TUIIntegrationHarness) Close() {
	h.cancel()
}

func (h *TUIIntegrationHarness) SendKey(k string) {
	h.TUIProgram.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/tui/harness_test.go
git commit -m "test: add TUIIntegrationHarness for integration testing"
```

---

### Task 3: Implement Blocking Interactive Test

**Files:**
- Create: `internal/tui/integration_test.go`

- [ ] **Step 1: Write the failing test for `send_interactive`**

```go
func TestIntegration_SendInteractive_Blocks(t *testing.T) {
	h := NewHarness(t)
	defer h.Close()

	resChan := make(chan any)
	errChan := make(chan error)

	go func() {
		res, err := h.Registry.Call(context.Background(), "tui.notify_send_interactive", map[string]any{
			"prompt": "Continue?",
			"options": []string{"Yes", "No"},
		})
		resChan <- res
		errChan <- err
	}()

	// Verify it's displayed (Check model state)
	if !h.TUIModel.content.hasActiveQA() {
		t.Errorf("expected active QA in TUI model")
	}

	// Simulate user answer
	h.SendKey("1") // Select "Yes"

	select {
	case res := <-resChan:
		if res != "Yes" {
			t.Errorf("expected 'Yes', got %v", res)
		}
	case err := <-errChan:
		t.Errorf("unexpected error: %v", err)
	case <-time.After(2 * time.Second):
		t.Errorf("timeout waiting for interactive response")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails (it will currently return immediately)**

Run: `go test -v internal/tui/integration_test.go`
Expected: FAIL (it returns "interactive notification sent" instead of blocking)

- [ ] **Step 3: Modify `internal/tui/app.go` to make it blocking**

Update the `notify_send_interactive` tool handler to wait for a result. You'll need to add a response channel to the `interactiveNotificationMsg` or similar.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test -v internal/tui/integration_test.go`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/app.go internal/tui/integration_test.go
git commit -m "feat: make tui.notify_send_interactive blocking"
```

---

### Task 4: Implement Non-blocking Notification Test

**Files:**
- Modify: `internal/tui/integration_test.go`

- [ ] **Step 1: Write the test for `send`**

```go
func TestIntegration_Send_NonBlocking(t *testing.T) {
	h := NewHarness(t)
	defer h.Close()

	// Call it
	res, err := h.Registry.Call(context.Background(), "tui.notify_send", map[string]any{
		"message": "Hello World",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != "notification sent" {
		t.Errorf("unexpected result: %v", res)
	}

	// Verify it appeared in the model
	found := false
	for _, item := range h.TUIModel.content.items {
		if item.Text == "Hello World" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("message 'Hello World' not found in TUI content model")
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test -v internal/tui/integration_test.go`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/tui/integration_test.go
git commit -m "test: verify non-blocking behavior of tui.notify_send"
```

---

### Task 5: Implement Starlark Integration Test

**Files:**
- Modify: `internal/tui/integration_test.go`

- [ ] **Step 1: Write a test that executes a Starlark script through the harness**

```go
func TestIntegration_Starlark_Interaction(t *testing.T) {
	h := NewHarness(t)
	defer h.Close()

	it := interpreter.NewStarlark(h.Registry, zerolog.Nop())
	script := `
def main():
    ans = tui.notify_send_interactive(prompt="Ready?", options=["Ready", "Not Ready"])
    return ans
`
	intent := &orchestrator.Intent{ID: "test", Script: script}
	
	go func() {
		_ = it.Execute(context.Background(), intent, "", interpreter.RunOptions{RunID: "run-1"})
	}()

	// Wait for QA to appear
	// ... SendKey("1") ...
	// Verify result is "Ready"
}
```

- [ ] **Step 2: Run and verify**

Run: `go test -v internal/tui/integration_test.go`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/tui/integration_test.go
git commit -m "test: verify full starlark-to-tui integration"
```
