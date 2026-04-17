# Tech Stack: Clara

## Core Orchestration & Logic
- **Languages:** Go (1.26.1 - macOS, Linux), Swift (macOS Native Bridge), JavaScript (Chrome Extension).
- **Orchestration Logic:** Starlark (via `go.starlark.net`), providing deterministic, Python-like intent scripts.
- **Tooling Interface:** Model Context Protocol (MCP-Go), used for standardized tool registration and interaction.
- **Persistent Store:** SQLite (via `go-sqlite3` and `go-sqlite3/embed`), used for intent state persistence, MCP server configurations, and high-performance FTS5 search indexing.

## User Interface & Experience
- **Terminal Interface:** Bubble Tea (TUI Framework) for building rich, interactive CLI and dashboard.
- **TUI Styling:** Lipgloss for terminal-based styling and formatting.
- **CLI Framework:** Cobra (CLI application library) for building the `clara` command-line utility.

## Communication & Integrations
- **macOS Integration:** Swift-based ClaraBridge for native access to macOS APIs (e.g., Photos, Reminders).
- **Browser Automation:** JavaScript-based Chrome extension with Native Messaging support for reliable, persistent communication.
- **Event-Driven Bus:** Internal event bus for handling MCP notifications and state changes.

## Infrastructure & Logging
- **Logging Framework:** Zerolog, used for structured, high-performance logging.
- **Development Tooling:** Air (for hot reloading during Go development), Goreleaser (for building and releasing binaries).
- **Testing:** Standard Go `testing` package with a focus on unit and integration tests.
