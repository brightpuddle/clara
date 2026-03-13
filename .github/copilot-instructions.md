# Clara — Copilot Instructions

## Project Overview

**Clara** (`github.com/brightpuddle/clara`) is a local-first agentic orchestrator for macOS. It is a background daemon written in Go that:

1. Proxies and aggregates MCP (Model Context Protocol) servers into a unified tool registry.
2. Executes declarative **Intents** — JSON state machines authored by an LLM from natural-language Markdown intent files — using a safe, inspectable interpreter.
3. Persists its own operational state (runs, checkpoints, metadata) in an internal SQLite store that is **not** exposed directly to intents.
4. Treats every intent-visible capability as an MCP service, including built-in Clara services such as filesystem access, SQLite tooling, and the native macOS bridge.
5. Exposes a `cobra`-based CLI (`clara`) for daemon control, introspection, and launching built-in MCP servers.

The architectural rule is:

> If a capability is available to intents, it should be delivered through MCP.

Clara's Go daemon is therefore a **policy/orchestration engine**, not a grab-bag of directly embedded tools.

---

## Language & Runtime

- **Go 1.24+** for the daemon, CLI, and built-in MCP servers.
- **Swift 6.0+** for the standalone macOS MCP bridge.
- Go module name: `github.com/brightpuddle/clara`.

---

## Project Structure

```text
github.com/brightpuddle/clara/
├── cmd/
│   └── clara/           # Unified binary: daemon + CLI + built-in MCP launchers
│       ├── main.go      # cobra root command, shared helpers
│       ├── serve.go     # clara serve (daemon logic)
│       ├── agent.go     # clara agent {start,stop,status}
│       ├── intent.go    # clara intent {list,run}
│       ├── run.go       # clara run <task-file>
│       ├── tool.go      # clara tool {list,show,call}
│       └── mcp.go       # clara mcp {fs,db,...}
├── internal/
│   ├── config/          # Config loader (config.yaml + os.ExpandEnv)
│   ├── orchestrator/    # Intent, State, Transition structs (core domain types)
│   ├── registry/        # MCP client management + unified tool registry
│   ├── interpreter/     # State machine Execute loop (expr-lang/expr conditions)
│   ├── supervisor/      # File watcher + LLM→Intent conversion + lifecycle management
│   ├── mcpserver/       # Built-in MCP servers
│   │   ├── fs/          # Filesystem MCP server (clara mcp fs)
│   │   └── db/          # SQLite MCP server (clara mcp db)
│   └── store/           # Internal daemon persistence only (intent_runs, metadata)
├── swift/               # Standalone Swift MCP server for native macOS capabilities
│   ├── Package.swift
│   └── Sources/ClaraBridge/
├── config.yaml.example
└── go.mod
```

Note: during migrations, legacy bridge/proto artifacts may still exist in the repo. Prefer the MCP-first architecture above when making changes.

---

## Core Dependencies

| Purpose | Library |
|---|---|
| Structured logging | `github.com/rs/zerolog` |
| Error handling (with stacktraces) | `github.com/cockroachdb/errors` |
| SQLite (CGO-free) | `github.com/ncruces/go-sqlite3` |
| Vector search extension | `github.com/asg017/sqlite-vec-go-bindings/ncruces` |
| State machine logic evaluation | `github.com/expr-lang/expr` |
| CLI | `github.com/spf13/cobra` |
| MCP client/server | `github.com/mark3labs/mcp-go` |
| Structured concurrency | `github.com/sourcegraph/conc` |
| YAML config parsing | `gopkg.in/yaml.v3` |

---

## Architectural Principles

- **Intent-visible services must be MCP services.** Do not add new direct daemon-only tools for use in intents.
- **The internal store is private.** Clara's internal SQLite database exists for orchestration/runtime persistence only.
- **Built-in services are still services.** Even when shipped inside the Clara repo/binary (for example `clara mcp fs` or `clara mcp db`), they should behave like standalone MCP servers and be usable independently of the daemon.
- **Prefer service composition over special cases.** If a new capability can be expressed as an MCP server, do that instead of wiring custom transport paths into the daemon.
- **Keep the daemon simple.** Its responsibilities are config loading, subprocess orchestration, intent execution, state persistence, and policy enforcement across MCP services.

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
- MCP servers must avoid writing human-oriented logs to stdout because stdout is reserved for the protocol. Use stderr for diagnostics when needed.

---

## Configuration

- Config is loaded from `config.yaml` (default path: `~/.config/clara/config.yaml`).
- Runtime data (internal DB, control socket, logs, tasks) is stored under `~/.local/share/clara/` by default.
- Intent-visible services are configured under `mcp_servers`.
- Use `os.ExpandEnv` when parsing string values to support `${ENV_VAR}` credential injection.
- **Never** commit real credentials or API keys. The `config.yaml.example` file shows only placeholder `${VAR}` references.

Example service entries:

```yaml
mcp_servers:
  - name: fs
    command: clara
    args: [mcp, fs]

  - name: db
    command: clara
    args: [mcp, db, ${HOME}/.local/share/clara/data.db]

  - name: bridge
    command: /usr/local/bin/ClaraBridge
    args: []
```

---

## Concurrency

- Use `github.com/sourcegraph/conc` for structured goroutine management (pools, wait groups with panic recovery).
- Goroutine lifetimes must always be bounded by a `context.Context`. Never fire-and-forget without supervision.
- The `mem` map inside the interpreter's `Execute` loop is local to a single run and is not shared across goroutines. Do not introduce shared mutable state without explicit synchronization.

---

## SQLite / sqlite-vec

- Use `github.com/ncruces/go-sqlite3` (CGO-free, pure Go WASM backend) — this is required for cross-compilation.
- Enable the `sqlite-vec` extension via `github.com/asg017/sqlite-vec-go-bindings/ncruces`.
- Clara's **internal** daemon DB lives at `~/.local/share/clara/clara.db` by default and stores orchestration state only.
- The built-in SQLite MCP server (`clara mcp db`) is a **separate** service and may point at its own file path or default to an in-memory database.
- Vector tables use the `vec0` virtual table interface.

---

## Testing

- 100% test coverage is **not** a goal, but critical code paths must have good test coverage. Focus on:
  - `internal/interpreter`: the `Execute` loop, transition evaluation, `Wait` mechanism.
  - `internal/config`: config loading and env var expansion.
  - `internal/orchestrator`: Intent and State struct validation.
  - `internal/registry`: MCP server registration, discovery, and dispatch.
  - `internal/mcpserver/*`: built-in MCP tool behavior for filesystem, SQLite, and future services.
- Use Go's standard `testing` package. Prefer table-driven tests.
- Use `testify` (`github.com/stretchr/testify`) only if it meaningfully reduces boilerplate; the standard library is preferred.
- Tests must not require network access or external services. Use interfaces and test doubles for MCP clients.

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

- `action` maps to a registered MCP-derived tool in the Registry.
- `args` values support `{{handlebars}}`-style template injection from `mem`.
- `transitions` are evaluated in order using `expr-lang/expr` against the current `mem` map.
- The special `PROMPT_USER` pattern uses a "Wait" mechanism that suspends the goroutine until external input arrives via a channel.

---

## CLI Commands (`clara`)

| Command | Description |
|---|---|
| `clara` | Interactive TUI HUD (placeholder: shows agent status) |
| `clara serve` | Start the background agent in the foreground |
| `clara agent start` | Check/report agent status; print instructions to start |
| `clara agent stop` | Stop the running agent |
| `clara agent status` | Show agent status and active intents |
| `clara intent list` | List all active intents |
| `clara intent run <id>` | Manually trigger an intent by ID |
| `clara run <task-file>` | One-off execution of a JSON intent file |
| `clara tool list` | List all registered tools with signatures |
| `clara tool show <tool>` | Show full MCP-style details for one tool |
| `clara tool call <tool> ...` | Call a registered tool directly |
| `clara mcp fs` | Start the built-in filesystem MCP server on stdio |
| `clara mcp db [path]` | Start the built-in SQLite MCP server on stdio |

CLI is implemented with `github.com/spf13/cobra`. All commands live in `cmd/clara/` as a single unified binary — there is no separate `clarad` daemon binary.

---

## Swift Bridge

- `ClaraBridge` is a **standalone Swift MCP server**.
- It communicates over **stdio**, not gRPC.
- Configure it under `mcp_servers` like any other service.
- Initial capability: `fetch_reminders` via EventKit.
- Future native capabilities (Notifications, Spotlight, filesystem events, etc.) should also be exposed as MCP tools/resources/prompts rather than custom daemon transports.
- Swift 6.0 strict concurrency model must be followed (no `@unchecked Sendable` shortcuts).
