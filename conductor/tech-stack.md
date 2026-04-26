# Clara Tech Stack

- **Primary Language:** Go 1.24+ (Daemon, CLI, built-in MCP servers).
- **Secondary Language:** Swift 6.0+ (macOS-native bridge).
- **Orchestration Logic:** Native Go (via `hashicorp/go-plugin`), providing
  deterministic, compiled intent plugins.
- **Protocol:** Model Context Protocol (MCP) for tool aggregation and
  communication.
- **Persistence:** SQLite (via `ncruces/go-sqlite3`, a CGO-free driver).
- **CLI Framework:** Cobra.
- **TUI Framework:** Bubble Tea (Charmbracelet).
- **Expression Language:** `expr-lang/expr` (for state machine transition
  logic).
- **Logging:** Zerolog (structured, JSON-first logging).
- **Error Handling:** `cockroachdb/errors` (rich stack traces).
- **Platforms:** macOS, Linux.
