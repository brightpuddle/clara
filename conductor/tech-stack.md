# Clara Tech Stack

- **Primary Language:** Go 1.24+ (Daemon, CLI, integration plugins).
- **Secondary Language:** Swift 6.0+ (macOS-native gRPC bridge).
- **Intent Authoring:** Starlark (`.star` scripts interpreted by `go.starlark.net/starlark`).
- **Integration Transport:** `hashicorp/go-plugin` (net/RPC for Go plugins, gRPC for the Swift bridge).
- **Tool Spec & Dispatch:** `mcp-go` — used internally within integration plugins to describe and
  dispatch tools; it is not the daemon↔plugin transport.
- **Persistence:** SQLite (via `ncruces/go-sqlite3`, a CGO-free driver).
- **CLI Framework:** Cobra.
- **TUI Framework:** Bubble Tea (Charmbracelet).
- **Expression Language:** `expr-lang/expr` (for state machine transition logic).
- **Logging:** Zerolog (structured, JSON-first logging).
- **Error Handling:** `cockroachdb/errors` (rich stack traces).
- **Platforms:** macOS, Linux.
