# TUI Notification End-to-End Assurance Plan

## Objective
Implement a comprehensive end-to-end test suite for TUI notifications to ensure bidirectional behavior works correctly in all scenarios (TUI open/closed, Starlark scripts, CLI tool calls) and to prevent regressions like the "duplicate prompt" issue.

## Scenarios to Cover

### 1. TUI Open Scenarios
- [ ] `tui.notify.send` sends notification to the TUI.
- [ ] `clara tool call tui.notify.send` sends notification to the TUI and completes.
- [ ] `tui.notify.send_interactive` (Starlark) blocks until user responds in TUI.
- [ ] Script resumes correctly with the user's response.
- [ ] `clara tool call tui.notify.send_interactive` blocks until TUI response and logs it.

### 2. TUI Offline Scenarios
- [ ] `tui.notify.send` logs to intent history and continues without error.
- [ ] `clara tool call tui.notify.send` logs message and terminates.
- [ ] `tui.notify.send_interactive` (Starlark) pauses script and queues in DB.
- [ ] Script resumes with correct history replay when TUI eventually answers.
- [ ] `clara tool call tui.notify.send_interactive` blocks until TUI opens and answers.
- [ ] Breaking out of the CLI call (Ctrl+C) removes the queued notification from DB.

### 3. Persistence & Replay Scenarios
- [ ] Queued notifications are presented in oldest-first sequence when TUI opens.
- [ ] Answered notifications are NEVER presented again.
- [ ] Starlark replay correctly skips already-answered interactive prompts.
- [ ] No "waiting for resume input" errors for non-interactive notifications.

## Implementation Steps

### Task 1: Enhance Testing Infrastructure
- [ ] Create `internal/tui/e2e_harness_test.go` with `E2EHarness`.
- [ ] `E2EHarness` should include:
    - Real `store.Store` (SQLite).
    - Daemon registry with permanent TUI tool proxies.
    - Mock IPC server handling `tool_call`, `tui.history`, `start`, etc.
    - Controlled TUI instance that can be started/stopped and connect to the harness.
    - Helper to simulate `clara tool call`.

### Task 2: Implement Test Suite
- [ ] Implement `internal/tui/e2e_notification_test.go` covering all scenarios.
- [ ] Ensure tests are deterministic and handle timeouts appropriately.

### Task 3: Root Cause & Fix
- [ ] Run the suite and identify failing tests.
- [ ] Analyze `cmd/clara/serve.go` and `internal/tui/app.go` logic.
- [ ] Fix identified issues (e.g., duplicate entries, incorrect replay, missing DB cleanup).

### Task 4: Verification
- [ ] All tests pass.
- [ ] Manual verification of the reported issue.

## Verification & Testing
- `go test -v ./internal/tui/ -run TestE2E_`
- `go test -v ./internal/store/`
- `go test -v ./internal/interpreter/`
