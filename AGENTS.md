# Clara — Agent Instructions

## Project Overview

**Clara** (`github.com/brightpuddle/clara`) is an efficient, reliable agentic
orchestrator. It is a background daemon written in Go that:

1. Loads **Integration plugins** (native Go binaries built with
   `hashicorp/go-plugin`) that expose typed tools into a unified registry.
2. Executes **Intents** authored as **Starlark scripts** (`.star` files) that
   call registered tools.
3. Persists operational state (runs, checkpoints, metadata) in an internal
   SQLite store.
4. Exposes a `cobra`-based CLI (`clara`) for daemon control, introspection, and
   live intent management.

**Architectural rules:**
- **Integrations expose tools; Starlark scripts consume them.** Integration
  plugins (go-plugin RPC/gRPC) register tools into the registry; Starlark
  intents call those tools via namespace proxies (`fs.read_file(...)`, etc.).
- **MCP (`mcp-go`) is used internally** within integration plugins to describe
  and dispatch tools, but it is *not* the transport between the daemon and
  plugins — that transport is go-plugin net/RPC (or gRPC for the Swift bridge).

- **Go 1.24+** for the daemon, CLI, and integration plugins (supports macOS and
  Linux).
- **Swift 6.0+** for the standalone macOS gRPC bridge (`swift/`).
- Go module: `github.com/brightpuddle/clara`

---

## Build / Lint / Test Commands

```bash
# Build
make build          # builds clara and all plugins in ./bin/
go build ./cmd/clara

# Test (all)
make test           # go test ./... -timeout 60s

# Run a single test
go test ./internal/config -run TestLoad_BasicParsing -v
go test ./internal/orchestrator -run TestValidate -v
# Pattern: go test ./<package-path> -run <TestFunctionName> -v

# Vet
make vet            # go vet ./...

# Lint
make lint           # staticcheck ./...

# Format (run before committing)
make fmt            # golines -m 100 --base-formatter goimports -w ./...
                    # goimports -w ./...

# Dependencies
go mod tidy         # after adding/removing dependencies

# Build Swift bridge (macOS only)
make bridge

# Install as macOS LaunchAgent
make install
make uninstall
```

All committed code must pass `go vet ./...` and `staticcheck ./...` with no
warnings or errors.

---

## Project Structure

```
cmd/
  clara/            # Unified binary: daemon + CLI
    main.go         # cobra root, shared helpers
    serve.go        # clara serve (daemon)
    agent.go        # clara agent {start,stop,status,logs}
    intent.go       # clara intent {list,run,trigger,...}
    tool.go         # clara tool {list,show,call}
    plugin.go       # clara plugin {list,load,unload,reload}
    plugins.go      # pluginLoader: integration discovery, .star hot-reload
  integrations/     # Native Go integration plugins (go-plugin RPC)
    fs/             # Filesystem tools
    db/             # SQLite tools
    llm/            # LLM multiplexer (Gemini, Ollama, etc.)
    shell/          # Local shell execution
    web/            # Web search (DuckDuckGo)
    chrome/         # Browser automation
    zk/             # Zettelkasten/Obsidian vault tools
internal/
  config/           # Config loader (~/.config/clara/config.yaml + os.ExpandEnv)
  orchestrator/     # Intent, State, Transition types + Starlark value helpers
  registry/         # Unified tool registry (namespace → tool callables)
  interpreter/      # State machine executor (expr-lang) + Starlark executor
  supervisor/       # Intent lifecycle management (scheduling, events)
  store/            # Internal daemon persistence (SQLite)
  tui/              # Interactive TUI (bubbletea)
  ipc/              # Unix-socket IPC between CLI and daemon
pkg/
  contract/         # go-plugin RPC/gRPC contracts and handshake config
swift/              # Standalone Swift gRPC integration bridge (ClaraBridge / macOS)
extension/          # Chrome extension source
```

---

## Code Style & Formatting

### Formatting

- **Line length:** 100 characters (`golines -m 100`).
- **Formatter:** `golines` + `goimports` (run `make fmt` before committing).
- Standard Go formatting rules apply: tabs for indentation, no semicolons,
  double quotes for strings.
- Struct field tags should be column-aligned within a struct definition.

### Imports

Use three groups separated by blank lines:

```go
import (
    "context"
    "fmt"
    "os"

    "github.com/cockroachdb/errors"
    "github.com/rs/zerolog"

    "github.com/brightpuddle/clara/internal/config"
)
```

Order: standard library → third-party → internal
(`github.com/brightpuddle/clara/...`).

### Naming Conventions

- **Files:** `snake_case.go` (e.g., `mcp_server.go`).
- **Packages:** short, lowercase, no underscores (e.g., `config`,
  `orchestrator`, `mcpserver`).
- **Exported identifiers:** `CamelCase` (e.g., `Intent`, `Validate`,
  `NewRegistry`).
- **Unexported identifiers:** `lowerCamelCase` (e.g., `loadConfig`,
  `parseIntent`).
- **Constants:** `CamelCase` for exported (e.g., `WorkflowTypeNative`,
  `IntentModeOnDemand`).
- Constructor functions: `New<Type>(...)`.

### Types

- Use `any` (not `interface{}`) for generic values (Go 1.18+).
- Use strongly-typed structs with JSON/YAML tags for serialization.
- Avoid heavy reflection; prefer explicit wiring via function parameters and
  closures.
- Integration plugins implement `contract.Integration` (from `pkg/contract`).
- Starlark intents are plain `.star` files; no Go compilation is required.

---

## Error Handling

Wrap **all** errors with `github.com/cockroachdb/errors` to capture stack
traces:

```go
import "github.com/cockroachdb/errors"

// Wrap existing errors
return errors.Wrap(err, "failed to load config")

// Create new errors with formatting
return errors.Newf("unsupported mode: %q", mode)
```

- Never silently swallow errors. Handle, wrap+return, or log with explicit
  intent.
- Use `errors.Is` / `errors.As` for inspection; never compare error strings
  directly.
- `fmt.Errorf("%w", err)` is acceptable in low-level CLI helpers, but prefer
  `cockroachdb/errors` everywhere else.

---

## Logging

Use `github.com/rs/zerolog` for all structured logging:

```go
log.Info().Str("intent_id", id).Str("state", state).Msg("transitioning")
log.Error().Err(err).Str("tool", toolName).Msg("tool call failed")
```

- Log levels: `Trace`, `Debug`, `Info`, `Warn`, `Error`, `Fatal`.
- Always include contextual fields (e.g., `intent_id`, `state`, `tool`) — not
  just a message string.
- **Integration plugins must never write to stdout** — stdout is reserved for
  the go-plugin RPC/gRPC framing. Use stderr for diagnostics when needed.
- The daemon logs to a file by default (`~/.local/share/clara/clara.log`); use
  stderr for dev/foreground mode.

---

## Concurrency

- Use `github.com/sourcegraph/conc` for structured goroutine management (pools,
  wait groups with panic recovery).
- All goroutine lifetimes must be bounded by a `context.Context`. Never
  fire-and-forget.
- The interpreter's `Execute` loop `mem` map is local to a single run; do not
  introduce shared mutable state without explicit synchronization.

---

## Testing

- Use Go's standard `testing` package. Prefer table-driven tests.
- Use `testify` (`github.com/stretchr/testify`) only if it meaningfully reduces
  boilerplate.
- Tests must not require network access or external services. Use interfaces and
  test doubles for MCP clients.
- Focus coverage on: `internal/interpreter`, `internal/config`,
  `internal/orchestrator`, `internal/registry`, `cmd/integrations/*`.
- Use `t.TempDir()`, `t.Setenv()`, etc. for test isolation.

```go
func TestLoad_BasicParsing(t *testing.T) {
    cases := []struct {
        name  string
        input string
        want  *Config
    }{
        {"minimal", `key: value`, &Config{Key: "value"}},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            // ...
        })
    }
}
```

---

## Dependencies

- Standard Go modules (`go.mod` / `go.sum`). No vendor directories.
- Pin to specific versions. Run `go mod tidy` after changes.
- Prefer the standard library; add third-party libraries only for clear value.

Key dependencies:

| Purpose                          | Library                         |
| -------------------------------- | ------------------------------- |
| Structured logging               | `github.com/rs/zerolog`         |
| Error handling (stacktraces)     | `github.com/cockroachdb/errors` |
| SQLite (CGO-free)                | `github.com/ncruces/go-sqlite3` |
| State machine conditions         | `github.com/expr-lang/expr`     |
| Integration plugins (RPC/gRPC)   | `github.com/hashicorp/go-plugin`|
| CLI                              | `github.com/spf13/cobra`        |
| Tool spec & dispatch (internal)  | `github.com/mark3labs/mcp-go`   |
| Starlark intent interpreter      | `go.starlark.net/starlark`      |
| Structured concurrency           | `github.com/sourcegraph/conc`   |
| YAML parsing                     | `gopkg.in/yaml.v3`              |
| Filesystem watching (hot-reload) | `github.com/fsnotify/fsnotify`  |

---

## Architectural Principles

- **Integrations expose tools; Starlark scripts consume them.** To add a new
  capability, create an integration plugin in `cmd/integrations/<name>/` that
  implements `contract.Integration`, then call it from Starlark.
- **The internal store is private.** Clara's SQLite DB is for orchestration
  persistence only; it is not exposed to integrations or intents.
- **go-plugin is the integration transport.** The daemon communicates with
  integration binaries via `hashicorp/go-plugin` (net/RPC or gRPC). Do not
  bypass this with direct subprocess pipes or custom sockets.
- **MCP (`mcp-go`) is used inside integration plugins** to describe and dispatch
  tools, but it is not the daemon↔plugin transport.
- **Keep docs in sync.** Feature and architectural changes must update
  `README.md` and `AGENTS.md`.

## Project Sources of Truth

The primary sources of truth for project rules, architecture, and workflow are
maintained in the `conductor/` directory:

- [Product Definition](conductor/product.md)
- [Tech Stack](conductor/tech-stack.md)
- [Workflow](conductor/workflow.md)
