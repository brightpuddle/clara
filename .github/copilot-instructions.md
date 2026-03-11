# Clara — Copilot Instructions

## Project Overview

**Clara** (`github.com/brightpuddle/clara`) is a local-first agentic orchestrator for macOS. It is a background daemon written in Go that:

1. Proxies and aggregates MCP (Model Context Protocol) servers into a unified tool registry.
2. Executes declarative **Intents** — JSON state machines authored by an LLM from natural-language Markdown intent files — using a safe, inspectable interpreter.
3. Provides a native macOS capability bridge via a separate Swift binary communicating over gRPC/Unix Domain Socket (EventKit, FileSystem events, CoreSpotlight).
4. Persists task state and vector embeddings in a CGO-free SQLite instance (with `sqlite-vec`).
5. Exposes a `cobra`-based CLI (`clara`) for daemon control and introspection.

---

## Language & Runtime

- **Go 1.24+** for the daemon and CLI.
- **Swift 6.0+** for the native macOS bridge.
- Go module name: `github.com/brightpuddle/clara`.

---

## Project Structure

```
github.com/brightpuddle/clara/
├── cmd/
│   ├── clarad/          # Daemon binary entry point
│   └── clara/           # CLI binary entry point
├── internal/
│   ├── config/          # Config loader (config.yaml + os.ExpandEnv)
│   ├── orchestrator/    # Intent, State, Transition structs (core domain types)
│   ├── registry/        # Tool registry: MCP client management + Swift bridge wrapper
│   ├── interpreter/     # State machine Execute loop (expr-lang/expr conditions)
│   ├── supervisor/      # File watcher + LLM→Intent conversion + lifecycle management
│   └── bridge/          # gRPC client for the Swift native bridge
├── proto/
│   └── bridge.proto     # BridgeService protobuf definition
├── swift/               # Swift native bridge (separate binary, managed by daemon)
│   ├── Package.swift
│   └── Sources/ClaraBridge/
├── config.yaml.example
└── go.mod
```

---

## Core Dependencies

| Purpose | Library |
|---|---|
| Structured logging | `github.com/rs/zerolog` |
| Error handling (with stacktraces) | `github.com/cockroachdb/errors` |
| SQLite (CGO-free) | `github.com/ncruces/go-sqlite3` |
| Vector search extension | `github.com/asg017/sqlite-vec-go-bindings/ncruces` |
| State machine logic evaluation | `github.com/expr-lang/expr` |
| CLI | `github.com/alecthomas/kong` |
| MCP client | `github.com/mark3labs/mcp-go` |
| Structured concurrency | `github.com/sourcegraph/conc` |
| gRPC | `google.golang.org/grpc` |
| Protobuf codegen | `google.golang.org/protobuf` |
| YAML config parsing | `gopkg.in/yaml.v3` |

---

## Code Style & Formatting

- **Formatters:** `golines` (line length 100) and `goimports`. All committed code must be formatted with both.
- **Linters:** All code must pass `staticcheck` and `go vet` with no warnings or errors.
- Follow standard Go idioms and the [Effective Go](https://go.dev/doc/effective_go) guidelines.
- Avoid "magic" frameworks (e.g. reflection-heavy DI containers). Prefer explicit wiring via function parameters and closures.
- Keep packages focused and minimal. Avoid circular imports.
- Use `internal/` packages to enforce encapsulation boundaries.

---

## Dependency Management

- Use the **standard Go module system** (`go.mod` / `go.sum`). Do not use vendor directories unless a specific dependency requires it.
- Pin dependencies to specific versions. Run `go mod tidy` after adding or removing dependencies.
- Prefer the standard library for simple tasks; add third-party libraries only when they provide clear, meaningful value.

---

## Error Handling

- Wrap **all** errors with `github.com/cockroachdb/errors` to capture stack traces:
  ```go
  return errors.Wrap(err, "failed to load config")
  ```
- Never silently swallow errors. Either handle them, wrap and return them, or log and continue with explicit intent.
- Use `errors.Is` / `errors.As` for error inspection; do not compare error strings.

---

## Logging

- Use `github.com/rs/zerolog` for all structured logging.
- Log levels: `Trace` (verbose debug), `Debug`, `Info`, `Warn`, `Error`, `Fatal`.
- Always include contextual fields (e.g. `intent_id`, `state`, `tool`) in log events — not just a message string.
- The daemon runs as a background process; log to a file by default, with an option to log to stderr for development.

---

## Configuration

- Config is loaded from `config.yaml` (default path: `~/.config/clara/config.yaml`).
- Runtime data (DB, sockets, logs) is stored under `~/.local/share/clara/`.
- Use `os.ExpandEnv` when parsing string values to support `${ENV_VAR}` credential injection.
- **Never** commit real credentials or API keys. The `config.yaml.example` file shows only placeholder `${VAR}` references.

---

## Concurrency

- Use `github.com/sourcegraph/conc` for structured goroutine management (pools, wait groups with panic recovery).
- Goroutine lifetimes must always be bounded by a `context.Context`. Never fire-and-forget without supervision.
- The `mem` map inside the interpreter's `Execute` loop is local to a single run and is not shared across goroutines. Do not introduce shared mutable state without explicit synchronization.

---

## gRPC / Proto

- The Swift bridge communicates with the Go daemon over a **Unix Domain Socket** using gRPC.
- Proto files live in `proto/`. Generated Go code lives in `internal/bridge/gen/` (committed to the repo).
- Regenerate with: `protoc --go_out=. --go-grpc_out=. proto/bridge.proto`

---

## SQLite / sqlite-vec

- Use `github.com/ncruces/go-sqlite3` (CGO-free, pure Go WASM backend) — this is required for cross-compilation.
- Enable the `sqlite-vec` extension via `github.com/asg017/sqlite-vec-go-bindings/ncruces`.
- DB file path: `~/.local/share/clara/clara.db`.
- Vector tables use the `vec0` virtual table interface. Wrap SQL queries in the `registry` Tool interface so the interpreter never writes raw SQL.

---

## Testing

- 100% test coverage is **not** a goal, but critical code paths must have good test coverage. Focus on:
  - `internal/interpreter`: the `Execute` loop, transition evaluation, `Wait` mechanism.
  - `internal/config`: config loading and env var expansion.
  - `internal/orchestrator`: Intent and State struct validation.
  - `internal/registry`: tool registration and dispatch.
- Use Go's standard `testing` package. Prefer table-driven tests.
- Use `testify` (`github.com/stretchr/testify`) only if it meaningfully reduces boilerplate; the standard library is preferred.
- Tests must not require network access or external services. Use interfaces and test doubles for MCP clients and the bridge.

---

## The Intent Schema

An **Intent** is a JSON document representing a state machine. The Go daemon is the interpreter; the LLM is only ever the author.

```json
{
  "id": "string",
  "description": "string",
  "schedule": "cron expression (optional)",
  "trigger": "event expression (optional)",
  "initial_state": "STATE_NAME",
  "context": {
    "alias": "mcp://server/resource"
  },
  "states": {
    "STATE_NAME": {
      "action": "tool.method",
      "args": { "key": "{{mem.PREV_STATE.field}}" },
      "transitions": [
        { "condition": "expr string", "next": "OTHER_STATE" }
      ],
      "next": "DEFAULT_NEXT_STATE",
      "terminal": false
    }
  }
}
```

- `action` maps to a registered Tool in the Registry.
- `args` values support `{{handlebars}}`-style template injection from `mem`.
- `transitions` are evaluated in order using `expr-lang/expr` against the current `mem` map.
- The special `PROMPT_USER` pattern uses a "Wait" mechanism that suspends the goroutine until external input arrives via a channel.

---

## CLI Commands (`clara`)

| Command | Description |
|---|---|
| `clara agent start` | Start the agent |
| `clara agent stop` | Stop the agent |
| `clara agent status` | Show agent status and active intents |
| `clara intent list` | List all active intents |
| `clara intent run <id>` | Manually trigger an intent by ID |
| `clara tool list` | List all registered tools |

CLI is implemented with `github.com/alecthomas/kong`.

---

## Swift Bridge

- The Swift bridge is a **separate binary** (`ClaraBridge`) managed as a subprocess by the Go daemon.
- It implements the `BridgeService` gRPC server on a Unix Domain Socket.
- Initial capability: `CallTool("fetch_reminders", args)` via EventKit.
- Swift 6.0 strict concurrency model must be followed (no `@unchecked Sendable` shortcuts).
- The Go daemon is responsible for starting and monitoring the Swift bridge process.
