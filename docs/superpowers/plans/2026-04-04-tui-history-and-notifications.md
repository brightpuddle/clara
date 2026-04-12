# TUI History and Notifications Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement persistence for TUI command and content history, and ensure TUI notification tools are always available to Starlark scripts even when the TUI is offline.

**Architecture:** 
- Database persistence for command and content history.
- Daemon-level ownership of `tui` tools.
- TUI client updates for history navigation and startup sync.
- `clara.wait` for offline interactive notifications.

**Tech Stack:** Go, SQLite, Bubble Tea, Starlark.

---

### Task 1: Database Migrations and Store Methods

**Files:**
- Modify: `internal/store/store.go`
- Test: `internal/store/store_test.go`

- [ ] **Step 1: Update `migrate` method to create TUI history tables**

```go
// In internal/store/store.go, update migrate()
`CREATE TABLE IF NOT EXISTS tui_command_history (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    command    TEXT    NOT NULL,
    created_at INTEGER NOT NULL DEFAULT (unixepoch())
);
CREATE INDEX IF NOT EXISTS idx_tui_command_history_created_at ON tui_command_history(created_at);

CREATE TABLE IF NOT EXISTS tui_content_history (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    type       TEXT    NOT NULL,
    text       TEXT    NOT NULL,
    data_json  TEXT    NOT NULL DEFAULT 'null',
    created_at INTEGER NOT NULL DEFAULT (unixepoch())
);
CREATE INDEX IF NOT EXISTS idx_tui_content_history_created_at ON tui_content_history(created_at);`
```

- [ ] **Step 2: Add TUI persistence methods to `Store`**

Implement:
- `SaveTUICommand(ctx, command)` (and prune to latest 100)
- `LoadTUICommandHistory(ctx)`
- `SaveTUIContent(ctx, item)`
- `LoadTUIContentHistory(ctx, limit)`
- `ClearTUIContentHistory(ctx)`

- [ ] **Step 3: Write and run tests for `Store` methods**

- [ ] **Step 4: Commit**

```bash
git add internal/store/store.go internal/store/store_test.go
git commit -m "feat(store): add tui history tables and methods"
```

---

### Task 2: Permanent Daemon-Owned TUI Tools

**Files:**
- Modify: `cmd/clara/serve.go`
- Modify: `cmd/clara/run.go` (if needed for one-off)

- [ ] **Step 1: Register permanent `tui` tools in `runDaemon`**

Instead of relying on dynamic registration only, register these in the daemon's registry. Use a proxy pattern that checks if a dynamic "tui" server is attached.

- [ ] **Step 2: Implement `tui.notify.send` logic**
1. Always log to `tui_content_history`.
2. Try to call the tool on the "tui" dynamic server if it exists.

- [ ] **Step 3: Implement `tui.notify.send_interactive` logic**
1. Always log to `tui_content_history`.
2. If TUI connected: Proxy.
3. If TUI offline: Return `PauseError` (via `MarkRunWaiting`).

- [ ] **Step 4: Verify with a script that calls `tui.notify.send` while TUI is offline**

- [ ] **Step 5: Commit**

```bash
git add cmd/clara/serve.go
git commit -m "feat(daemon): register permanent tui tools with history logging"
```

---

### Task 3: TUI Content History Loading

**Files:**
- Modify: `internal/tui/app.go`
- Modify: `internal/tui/client.go`

- [ ] **Step 1: Add `LoadContentHistory` to `IPCClient`**

Add an IPC method (or use `MethodToolCall` to `db.query`) to fetch the latest `tui_content_history`.

- [ ] **Step 2: Update `appModel.Init` to load history**

Fetch history from the client and populate `contentModel.items`.

- [ ] **Step 3: Verify TUI displays previous notifications on startup**

- [ ] **Step 4: Commit**

```bash
git add internal/tui/app.go internal/tui/client.go
git commit -m "feat(tui): load content history on startup"
```

---

### Task 4: Command History and Navigation in TUI

**Files:**
- Modify: `internal/tui/prompt.go`
- Modify: `internal/tui/app.go`

- [ ] **Step 1: Add `history` state to `promptModel`**

- [ ] **Step 2: Intercept `Up`/`Down` in `promptModel.Update`**

```go
// Boundary logic
if msg.Type == tea.KeyUp && m.input.Line() == 0 {
    // move to previous history entry
}
if msg.Type == tea.KeyDown && m.input.Line() == m.input.LineCount()-1 {
    // move to next history entry
}
```

- [ ] **Step 3: Save commands to history in `appModel.Update`**

When a command is submitted (likely on `Enter`), send an IPC request to save it to `tui_command_history`.

- [ ] **Step 4: Verify history navigation works as expected**

- [ ] **Step 5: Commit**

```bash
git add internal/tui/prompt.go internal/tui/app.go
git commit -m "feat(tui): persistent command history and navigation"
```

---

### Task 5: Interactive Notification Resume

**Files:**
- Modify: `internal/tui/app.go`

- [ ] **Step 1: TUI startup check for waiting runs**

Query the daemon for waiting runs that match a TUI prompt. If found, present it immediately in the TUI.

- [ ] **Step 2: Responding to QA sends input back to daemon**

When a QA is answered in the TUI, the client should send an `ipc.MethodStart` with `--input` to resume the waiting run.

- [ ] **Step 3: Verify full cycle (Script -> Pause -> TUI Answer -> Resume)**

- [ ] **Step 4: Commit**

```bash
git add internal/tui/app.go
git commit -m "feat(tui): resume interactive notifications from waiting state"
```
