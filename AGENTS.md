# Clara — Agent Instructions

## Project Overview

**Clara** (`github.com/brightpuddle/clara`) is a local-first agentic orchestrator for macOS. It is a background daemon written in Go that:

1. Proxies and aggregates MCP (Model Context Protocol) servers into a unified tool registry.
2. Executes **Intents** authored as `.star` Starlark workflows at runtime.
3. Persists operational state (runs, checkpoints, metadata) in an internal SQLite store.
4. Treats every intent-visible capability as an MCP service.
5. Exposes a `cobra`-based CLI (`clara`) for daemon control, introspection, and MCP gateway.

**Architectural rule:** If a capability is available to intents, it must be delivered through MCP.

- **Go 1.24+** for the daemon, CLI, and built-in MCP servers.
- **Swift 6.0+** for the standalone macOS MCP bridge (`swift/`).
- Go module: `github.com/brightpuddle/clara`

---

## Build / Lint / Test Commands

```bash
# Build
make build          # go build -o clara ./cmd/clara
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

All committed code must pass `go vet ./...` and `staticcheck ./...` with no warnings or errors.

---

## Project Structure

```
cmd/
  clara/            # Unified binary: daemon + CLI + built-in MCP launchers
    main.go         # cobra root, shared helpers
    serve.go        # clara serve (daemon)
    agent.go        # clara agent {start,stop,status,logs}
    intent.go       # clara intent {list,run,trigger,...}
    run.go          # clara run <task-file>
    tool.go         # clara tool {list,show,call}
    mcp.go          # clara mcp {fs,db,...}
    gateway.go      # clara gateway (aggregated MCP server)
internal/
  config/           # Config loader (config.yaml + os.ExpandEnv)
  orchestrator/     # Intent, State, Transition types (core domain)
  registry/         # MCP client management + unified tool registry
  interpreter/      # State machine Execute loop (expr-lang conditions)
  supervisor/       # .star file watcher + runtime lifecycle
  mcpserver/        # Built-in MCP servers (fs/, db/, ollamaembeddings/)
  store/            # Internal daemon persistence (SQLite)
  tui/              # Interactive TUI (bubbletea)
swift/              # Standalone Swift MCP bridge (ClaraBridge)
```

---

## Code Style & Formatting

### Formatting

- **Line length:** 100 characters (`golines -m 100`).
- **Formatter:** `golines` + `goimports` (run `make fmt` before committing).
- Standard Go formatting rules apply: tabs for indentation, no semicolons, double quotes for strings.
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

Order: standard library → third-party → internal (`github.com/brightpuddle/clara/...`).

### Naming Conventions

- **Files:** `snake_case.go` (e.g., `intent_loader.go`, `mcp_server.go`).
- **Packages:** short, lowercase, no underscores (e.g., `config`, `orchestrator`, `mcpserver`).
- **Exported identifiers:** `CamelCase` (e.g., `Intent`, `Validate`, `NewRegistry`).
- **Unexported identifiers:** `lowerCamelCase` (e.g., `loadConfig`, `parseIntent`).
- **Constants:** `CamelCase` for exported (e.g., `WorkflowTypeStarlark`, `IntentModeOnDemand`).
- Constructor functions: `New<Type>(...)`.

### Types

- Use `any` (not `interface{}`) for generic values (Go 1.18+).
- Use strongly-typed structs with JSON/YAML tags for serialization.
- Avoid heavy reflection; prefer explicit wiring via function parameters and closures.

---

## Error Handling

Wrap **all** errors with `github.com/cockroachdb/errors` to capture stack traces:

```go
import "github.com/cockroachdb/errors"

// Wrap existing errors
return errors.Wrap(err, "failed to load config")

// Create new errors with formatting
return errors.Newf("unsupported mode: %q", mode)
```

- Never silently swallow errors. Handle, wrap+return, or log with explicit intent.
- Use `errors.Is` / `errors.As` for inspection; never compare error strings directly.
- `fmt.Errorf("%w", err)` is acceptable in low-level CLI helpers, but prefer `cockroachdb/errors` everywhere else.

---

## Logging

Use `github.com/rs/zerolog` for all structured logging:

```go
log.Info().Str("intent_id", id).Str("state", state).Msg("transitioning")
log.Error().Err(err).Str("tool", toolName).Msg("tool call failed")
```

- Log levels: `Trace`, `Debug`, `Info`, `Warn`, `Error`, `Fatal`.
- Always include contextual fields (e.g., `intent_id`, `state`, `tool`) — not just a message string.
- **MCP servers must never write to stdout** — stdout is reserved for the MCP protocol. Use stderr for diagnostics when needed.
- The daemon logs to a file by default (`~/.local/share/clara/clara.log`); use stderr for dev/foreground mode.

---

## Concurrency

- Use `github.com/sourcegraph/conc` for structured goroutine management (pools, wait groups with panic recovery).
- All goroutine lifetimes must be bounded by a `context.Context`. Never fire-and-forget.
- The interpreter's `Execute` loop `mem` map is local to a single run; do not introduce shared mutable state without explicit synchronization.

---

## Testing

- Use Go's standard `testing` package. Prefer table-driven tests.
- Use `testify` (`github.com/stretchr/testify`) only if it meaningfully reduces boilerplate.
- Tests must not require network access or external services. Use interfaces and test doubles for MCP clients.
- Focus coverage on: `internal/interpreter`, `internal/config`, `internal/orchestrator`, `internal/registry`, `internal/mcpserver/*`.
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

| Purpose | Library |
|---|---|
| Structured logging | `github.com/rs/zerolog` |
| Error handling (stacktraces) | `github.com/cockroachdb/errors` |
| SQLite (CGO-free) | `github.com/ncruces/go-sqlite3` |
| State machine conditions | `github.com/expr-lang/expr` |
| Starlark runtime | `go.starlark.net` |
| CLI | `github.com/spf13/cobra` |
| MCP client/server | `github.com/mark3labs/mcp-go` |
| Structured concurrency | `github.com/sourcegraph/conc` |
| YAML parsing | `gopkg.in/yaml.v3` |

---

## Architectural Principles

- **Intent-visible services must be MCP services.** Do not add direct daemon-only tools for intents.
- **Authored intents are `.star` files.** Do not introduce new JSON/YAML/Markdown intent formats.
- **The internal store is private.** Clara's SQLite DB is for orchestration persistence only.
- **Gateway mode preserves protocol isolation.** `clara gateway` and MCP stdio commands must never write logs or human output to stdout.
- **Prefer service composition.** New capabilities → new MCP server, not custom transport paths.
- **Keep docs in sync.** Feature and architectural changes must update both `README.md` and `.github/copilot-instructions.md`.
