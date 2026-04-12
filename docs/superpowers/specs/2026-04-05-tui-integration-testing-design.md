# Design: TUI Integration Testing Harness

This document outlines the architecture for a Go-native integration testing framework for the Clara TUI, Daemon, and Starlark components. It aims to eliminate "whack-a-mole" regressions by verifying the full tool call lifecycle from the CLI/Starlark entry point to the TUI's Bubble Tea model.

## 1. Problem Statement
Changes to the TUI (Bubble Tea models), the Registry (MCP dispatch), or the Starlark interpreter often break the interdependent logic of user notifications. Specifically, blocking interactive prompts (`send_interactive`) and fire-and-forget notifications (`send`) lack behavioral coverage across process boundaries.

## 2. Goals
- **Full-Chain Validation:** Test the tool call path from Starlark/CLI -> Registry -> MCP Transport -> TUI Model.
- **Synchronous Interaction:** Verify that `send_interactive` blocks the caller until a user selection is injected.
- **Asynchronous Flow:** Verify that `send` is non-blocking and updates the TUI view if connected.
- **Replay Integrity:** Ensure that Starlark replay correctly skips previously resolved interactive prompts.
- **No External Dependencies:** Orchestrate all components within standard Go `go test` runs using in-memory mocks and pipes.

## 3. Architecture: `TUIIntegrationHarness`

The harness is a test helper that encapsulates the daemon's core logic and a "headless" instance of the TUI.

### 3.1 Components
- **Mock Registry:** A real `registry.Registry` instance with the `tui` namespace dynamically attached.
- **Virtual MCP Transport:** Uses `net.Pipe()` to connect the Registry's MCP client to the TUI's MCP server, simulating a Unix socket without file system side effects.
- **Headless TUI:**
    - Runs the actual `mcp.Tool` handlers from `internal/tui/app.go`.
    - Manages a real `appModel` (Bubble Tea model).
    - Uses a `tea.Model` wrapper that allows the test to "push" messages (keys, clicks) and "pull" the current view string.
- **Interpreter:** A `StarlarkInterpreter` configured to use the Mock Registry and a test-controlled history/replay store.

### 3.2 Data Flow (Interactive Call)
1. **Initiation:** Test runs `interpreter.Execute()` with a script: `res = tui.notify.send_interactive(prompt="ok?", options=["y", "n"])`.
2. **Blocking:** The Registry calls the TUI MCP tool. The TUI tool handler sends a message to the Bubble Tea program and **waits** for a response on a internal Go channel.
3. **Observation:** The test calls `harness.GetView()` and verifies the prompt "ok?" is present in the TUI's rendered output.
4. **Interaction:** The test calls `harness.SendKey("1")`. This simulates the user selecting the first option.
5. **Resolution:** The Bubble Tea model processes the key, triggers the callback, which sends the result back through the MCP pipe.
6. **Completion:** The Starlark script resumes, `res` is set to `"y"`, and the test verifies the final script state.

## 4. Testing Scenarios

### 4.1 `tui.notify.send_interactive`
- **Success:** Verify it blocks, returns the correct option, and updates the TUI state.
- **Timeout:** Verify the calling context can cancel a pending prompt.
- **Concurrency:** Verify that multiple scripts calling the TUI are handled correctly (e.g., serialized or queued).

### 4.2 `tui.notify.send`
- **Connected:** Verify it returns immediately and the message appears in the TUI view.
- **Disconnected:** Verify it returns success immediately even if no TUI is attached.

### 4.3 Starlark Replay
- **Persistence:** Verify that if an interactive call is "resolved" in the history, the script skips the TUI prompt entirely on the next run.

### 4.4 TUI Model (Bubble Tea)
- **State Transition:** Verify that the "Content" model correctly promotes a "QA" item to a "Notification" after it is answered.
- **Input Handling:** Verify that invalid keys (e.g., "0" or "a") do not resolve the interactive prompt.

## 5. Implementation Plan
1. **Refactor TUI App:** Extract the `mcp.MCPServer` construction logic from `Run()` into a factory function `NewTUIServer(p *tea.Program)` to allow sharing it between production and tests.
2. **Harness Setup:** Create `internal/tui/integration_test.go` with the `TUIIntegrationHarness` struct and lifecycle methods (`Start`, `Stop`, `SendKey`).
3. **Integration Tests:** Implement the scenarios listed in Section 4.
4. **Validation:** Run existing TUI tests to ensure no regressions in layout logic.

## 6. Verification
- Run `go test ./internal/tui/... -v` to confirm integration coverage.
- Add a Starlark script `tasks/test_integration.star` to manually verify the behavior in a live environment after the tests pass.
