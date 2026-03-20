# Clara — Copilot Instructions

## Project Overview

**Clara** (`github.com/brightpuddle/clara`) is a local-first agentic orchestrator for macOS. It is a background daemon written in Go that:

1. Proxies and aggregates MCP (Model Context Protocol) servers into a unified tool registry.
2. Executes **Intents** authored as `.star` Starlark workflows and compiled into a safe, inspectable runtime representation.
3. Persists its own operational state (runs, checkpoints, metadata) in an internal SQLite store that is **not** exposed directly to intents.
4. Treats every intent-visible capability as an MCP service, including built-in Clara services such as filesystem access, SQLite tooling, and the native macOS bridge.
5. Exposes a `cobra`-based CLI (`clara`) for daemon control, introspection, launching built-in MCP servers, and serving the aggregated registry as an MCP gateway.

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
│       ├── mcp.go       # clara mcp {fs,db,...}
│       └── gateway.go   # clara gateway (aggregated MCP server)
├── internal/
│   ├── config/          # Config loader (config.yaml + os.ExpandEnv)
│   ├── orchestrator/    # Intent, State, Transition structs (core domain types)
│   ├── registry/        # MCP client management + unified tool registry
│   ├── interpreter/     # State machine Execute loop (expr-lang/expr conditions)
│   ├── supervisor/      # .star file watcher + runtime mode/lifecycle management
 │   ├── mcpserver/       # Built-in MCP servers
 │   │   ├── fs/          # Filesystem MCP server (clara mcp fs)
 │   │   ├── db/          # SQLite MCP server (clara mcp db)
 │   │   ├── zk/          # Zettelkasten vault MCP server (clara mcp zk)
 │   │   └── ollamaembeddings/ # Ollama embeddings MCP server (clara mcp ollama-embeddings)
│   └── store/           # Internal daemon persistence only (intent_runs, metadata)
├── swift/               # Standalone Swift MCP server for native macOS capabilities
│   ├── Package.swift
│   └── Sources/ClaraBridge/
├── com.brightpuddle.clara.agent.plist # LaunchAgent plist for local macOS installation
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
| Starlark intent authoring/runtime | `go.starlark.net` |
| CLI | `github.com/spf13/cobra` |
| MCP client/server | `github.com/mark3labs/mcp-go` |
| Structured concurrency | `github.com/sourcegraph/conc` |
| YAML config parsing | `gopkg.in/yaml.v3` |

---

## Architectural Principles

- **Intent-visible services must be MCP services.** Do not add new direct daemon-only tools for use in intents.
- **Authored intents are `.star` files.** Do not introduce new authored JSON/YAML/Markdown intent formats unless the architecture explicitly changes.
- **The internal store is private.** Clara's internal SQLite database exists for orchestration/runtime persistence only.
- **Built-in services are still services.** Even when shipped inside the Clara repo/binary (for example `clara mcp fs` or `clara mcp db`), they should behave like standalone MCP servers and be usable independently of the daemon.
- **Gateway mode must preserve protocol isolation.** Commands that speak MCP over stdio, including `clara gateway`, must never write logs or human-readable output to stdout.
- **Prefer service composition over special cases.** If a new capability can be expressed as an MCP server, do that instead of wiring custom transport paths into the daemon.
- **Keep the daemon simple.** Its responsibilities are config loading, subprocess orchestration, intent execution, state persistence, and policy enforcement across MCP services.
- **Keep the docs in sync.** Feature changes and architectural changes must update both `README.md` and `.github/copilot-instructions.md`.

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
- Bare MCP server commands are resolved through `mcp_command_search_paths` plus common local binary directories and the ambient `PATH`, so launchd-managed Clara installs can still find tools like `clara`, `uvx`, or binaries installed under `${HOME}/go/bin`.
- Intent-visible services are configured under `mcp_servers`.
- Use `os.ExpandEnv` when parsing string values to support `${ENV_VAR}` credential injection.
- **Never** commit real credentials or API keys. The `config.yaml.example` file shows only placeholder `${VAR}` references.

Example service entries:

```yaml
mcp_command_search_paths:
  - ${HOME}/go/bin

mcp_servers:
  - name: fs
    command: clara
    args: [mcp, fs]

  - name: db
    command: clara
    args: [mcp, db, ${HOME}/.local/share/clara/data.db]

  - name: ollama
    command: clara
    args: [mcp, ollama-embeddings, --model, nomic-embed-text, --url, http://localhost:11434]

  - name: bridge
    command: /usr/local/bin/ClaraBridge
    args: []
```

Startup should be best-effort across configured MCP servers: one failing server should be logged and skipped without preventing the others from starting.

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

## Authored Intent Structure (`.star`)

Clara's authored intent format is `.star`.

Every intent file must either:

- define a callable `main()` (auto-registered as an on-demand task), or
- register at least one task with `task(...)`

**Intent ID**: always derived from the filename stem. There is no `id` field.

### `describe(text)`

Optional. Sets the human-readable description. May only be called once per file.

### `task(handler, *, trigger=, schedule=, interval=)`

Registers a handler as a task. The handler may be passed positionally or as
`handler=`. Mode is always inferred from the arguments:

- `trigger` present → `event`
- `schedule` present → `schedule`
- `interval` present → `worker`
- none of the above → `on_demand`

Multiple top-level `task(...)` calls register multiple tasks in a single file.
There is no wrapping `init()` call and no `mode` argument.

### Runtime builtins

- `describe(text)` — file-level description (compile-time only, no-op at runtime)
- `task(handler, ...)` — task registration (compile-time only, no-op at runtime)
- `tool(name, **kwargs)` — call a registered tool
- `wait(name, **kwargs)` — persist a wait request and resume later

### Minimal example

```python
# hello-world.star  →  intent id: "hello-world"
def main():
    return tool("fs.list_directory", path = ".")
```

### Multi-task example

```python
# reminders-sync.star
describe("React to reminder changes and run nightly syncs")

def on_reminder_change(event):
    return tool("taskwarrior.sync_reminder", reminder = event["item"])

def nightly_sync():
    return tool("taskwarrior.full_sync")

task(on_reminder_change, trigger = "bridge.reminders_changed")
task(nightly_sync, schedule = "0 2 * * *")
```

The daemon compiles `.star` files into the internal `orchestrator.Intent` runtime
representation. `.star` is the authored source of truth.

---

## CLI Commands (`clara`)

| Command | Description |
|---|---|
| `clara` | Interactive TUI HUD (placeholder: shows agent status) |
| `clara serve` | Start the background agent in the foreground |
| `clara agent start` | Start the LaunchAgent-managed daemon in the background |
| `clara agent stop` | Gracefully stop the daemon and unload its LaunchAgent |
| `clara agent status` | Show agent status and active intents |
| `clara agent logs [-w]` | Show recent daemon logs or follow them live |
| `clara intent list` | List installed intents (one row per task) |
| `clara intent start <id> [task]` | Start an intent task — fires a run for on-demand tasks, activates the persistent loop for schedule/worker/event |
| `clara intent start <id> --input '<json>'` | Deliver JSON input to the latest waiting run |
| `clara intent stop <id> [task]` | Stop a managed `schedule`, `worker`, or `event` task |
| `clara intent logs [id]` | Stream intent run events |
| `clara intent resume <run-id>` | Resume a paused Starlark run directly |
| `clara intent run <task-file>` | One-off execution of a `.star` intent file |
| `clara tool list` | List all registered tools with signatures |
| `clara tool show <tool>` | Show full MCP-style details for one tool |
| `clara tool call <tool> ...` | Call a registered tool directly |
| `clara gateway` | Start an MCP server that exposes the aggregated Clara tool registry on stdio |
| `clara mcp fs` | Start the built-in filesystem MCP server on stdio |
| `clara mcp db [path]` | Start the built-in SQLite MCP server on stdio |
| `clara mcp ollama-embeddings` | Start the built-in Ollama embeddings MCP server on stdio |
| `clara mcp taskwarrior` | Start the built-in Taskwarrior MCP server on stdio |
| `clara mcp zk <vault-path>` | Start the built-in Zettelkasten vault MCP server on stdio |

CLI is implemented with `github.com/spf13/cobra`. All commands live in `cmd/clara/` as a single unified binary — there is no separate `clarad` daemon binary.

Local installation notes:

- `make install` builds `clara`, installs it to `/usr/local/bin/clara`, installs `com.brightpuddle.clara.agent.plist` into `~/Library/LaunchAgents`, and restarts or starts the LaunchAgent-managed daemon.
- `make uninstall` stops Clara, removes the installed LaunchAgent plist, and removes `/usr/local/bin/clara`.
- `clara agent start` expects the LaunchAgent plist to be installed and should be the normal way to start the background service outside of development.
- `clara agent stop` sends the daemon a graceful shutdown request and then unloads the LaunchAgent so `KeepAlive` does not immediately restart it.
- Background daemon logs live at `~/.local/share/clara/clara.log`, rotate automatically, and can be viewed with `clara agent logs` or `clara agent logs --watch`.

TUI notes:

- Slash-command history is persisted under Clara's runtime data directory with bounded retention.
- `/tool call` autocomplete completes provider IDs first and tool suffixes after `provider.`.
- `/tool call <provider>` is a valid interactive shortcut that lists that provider's tools.

---

## Swift Bridge

- `ClaraBridge` is a **standalone Swift MCP server**.
- It communicates over **stdio**, not gRPC.
- Configure it under `mcp_servers` like any other service.
- It exposes reminder and calendar CRUD tools, notification tools, and blocking wait tools for reminder/event create-update-delete changes.
- `reminders_list` supports an `updated_after` ISO-8601 filter keyed to the serialized `updated_at` field.
- Those wait tools are backed by native EventKit change notifications and are intended for linear, event-driven `.star` scripts.
- Future native capabilities (Notifications, Spotlight, filesystem events, etc.) should also be exposed as MCP tools/resources/prompts rather than custom daemon transports.
- Swift 6.0 strict concurrency model must be followed (no `@unchecked Sendable` shortcuts).

## Built-in wait-capable tools

- `fs.wait_for_change` waits for filesystem create/change/delete events under a directory.
- `bridge.reminders_wait_change` waits for Reminders create/update/delete changes.
- `bridge.events_wait_change` waits for Calendar create/update/delete changes.
- `taskwarrior.list_tasks` supports an `updated_after` ISO-8601 filter keyed to Taskwarrior's `modified` field.
- Prefer exposing new event sources through comparable blocking MCP tools before inventing daemon-specific callback paths.
