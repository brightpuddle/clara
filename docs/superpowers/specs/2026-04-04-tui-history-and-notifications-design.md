# TUI History and Notifications Design Spec

**Date:** 2026-04-04
**Status:** Approved
**Topic:** Improving TUI history persistence and notification handling.

## 1. Overview
The Clara TUI (interactive HUD) currently lacks persistence for command and content history. Additionally, the `tui.notify` tools are registered dynamically, making them unavailable to Starlark scripts when the TUI is offline. This design introduces daemon-level ownership of TUI tools and database-backed persistence for history and missed notifications.

## 2. Goals
- Persistent command history (limit 100 entries) across sessions.
- Persistent content history (notifications, issues, QA) across restarts.
- `tui.notify` tools always available in Starlark, even if the TUI is offline.
- Offline interactive notifications (`send_interactive`) use the `clara.wait` mechanism to pause scripts until the TUI is opened and the prompt is answered.
- Improved command line navigation (Up/Down) through history.

## 3. Architecture

### 3.1 Database Schema (`internal/store/store.go`)
Two new tables will be added to the SQLite database:

- **`tui_command_history`**:
  - `id`: INTEGER PRIMARY KEY AUTOINCREMENT
  - `command`: TEXT NOT NULL
  - `created_at`: INTEGER NOT NULL DEFAULT (unixepoch())
  - *Index:* `idx_tui_command_history_created_at`

- **`tui_content_history`**:
  - `id`: INTEGER PRIMARY KEY AUTOINCREMENT
  - `type`: TEXT NOT NULL (e.g., "notification", "issue", "qa")
  - `text`: TEXT NOT NULL
  - `data_json`: TEXT NOT NULL DEFAULT 'null' (stores options, issue details, etc.)
  - `created_at`: INTEGER NOT NULL DEFAULT (unixepoch())
  - *Index:* `idx_tui_content_history_created_at`

### 3.2 Daemon Tool Registration (`cmd/clara/serve.go`)
The daemon will register permanent `tui` tools. These tools will:
1. Log the notification/issue/QA to `tui_content_history`.
2. If the TUI is connected (via dynamic MCP), proxy the call to the TUI.
3. If the TUI is offline and the call is `send_interactive`, return a `PauseError` (via `MarkRunWaiting`) to the interpreter.

### 3.3 TUI Integration (`internal/tui/`)
- **`appModel`**: On startup, loads the latest 50-100 items from `tui_content_history`.
- **`promptModel`**: 
  - Loads command history from the database.
  - Intercepts `tea.KeyMsg` for `Up` and `Down`.
  - Navigation Logic:
    - `Up`: If `input.Line() == 0`, move to previous history entry.
    - `Down`: If `input.Line() == input.LineCount()-1`, move to next history entry (or empty input).
- **Commands**: 
  - `/clear` or `Ctrl-L`: Sends an IPC request to the daemon to clear `tui_content_history`.

## 4. Implementation Strategy
1. **Migrations**: Update `internal/store/store.go` with new tables and indices.
2. **Persistence Methods**: Add `SaveTUICommand`, `LoadTUICommandHistory`, `SaveTUIContent`, `LoadTUIContentHistory`, and `ClearTUIContentHistory` to the `Store`.
3. **Daemon Tools**: Implement the permanent `tui` tools in `cmd/clara/serve.go`.
4. **TUI Prompt**: Update `promptModel` for history navigation.
5. **TUI App**: Update `appModel` to load history on init and handle persistence requests.

## 5. Verification Plan
- **Unit Tests**: Test new `Store` methods for history management.
- **Integration Tests**: Verify `tui.notify` calls from Starlark when TUI is offline (check DB state and script pausing).
- **Manual TUI Test**: Verify Up/Down history navigation and persistence across restarts.
