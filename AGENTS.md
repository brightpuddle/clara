# Clara — Agent Instructions

## Project Overview

**Clara** (`github.com/brightpuddle/clara`) is a lightweight, BEAM-style supervisor daemon written in Go that:

1. Ingests **CloudEvents** from multiple sources (Webex, Discord, email, filesystem changes, CLI prompts, system sensors) via a central **Event Bus**.
2. Evaluates smart **Trigger Rules** (nested boolean AST with `and`, `or`, `not`, dot-notation path extraction, regex, and comparison operators) to route events to external automation scripts.
3. Coordinates **Schedule Triggers** (cron / intervals) and **Worker Triggers** (supervised long-running background processes with configurable restart policies).
4. Supervises **External Scripts** (primarily TypeScript via Bun, alongside Python and shell binaries) in a "let it fail" process model, capturing stdout, stderr, execution durations, and exit codes into a local SQLite audit store.
5. Hosts a full **Model Context Protocol (MCP)** tool server (via HTTP/SSE and internal registry) exposing built-in tools (filesystem, database, shell, search, chrome automation, macOS bridge) and native Go plugins (`hashicorp/go-plugin`), with auto-generated TypeScript definitions (`clara types gen`).
6. Exposes a fast Unix-socket **CLI** and a modern browser-based **Web Management UI** (`/ui/` with Go Templ, Tailwind CSS v4, DaisyUI v5, HTMX, Alpine.js).

> **Exploratory Branch:** Zero backwards compatibility is required. Workflows and logic live in external scripts (e.g. TypeScript / Bun) calling MCP tools, while Clara itself focuses on robust supervision, routing, and tool hosting.

---

## Core Architecture

```
External Signals / Sensors
  (Webex, Discord, Email, Filesystem, CLI)
            │
            ▼
     ┌─────────────┐
     │  Event Bus  │ ◄─── CloudEvents (internal/supervisor/event_bus.go)
     └──────┬──────┘
            │
            ▼
┌─────────────────────────────────────────────────────────────┐
│                    Trigger Manager                          │  (internal/trigger/manager.go)
│                                                             │
│  • Event Rules (Boolean AST: and/or/not, dot paths, regex)  │  (internal/trigger/rule.go)
│  • Schedule Triggers (Cron / robfig cron v3)                │
│  • Supervised Workers (Restart policies & health tracking)  │
└───────────────────────────┬─────────────────────────────────┘
                            │ Spawns & Supervises (internal/trigger/runner.go)
                            ▼
               ┌────────────────────────┐
               │    External Scripts    │
               │  (Lua, Python, Shell)  │
               └────────────┬───────────┘
                            │ Calls MCP Tools (HTTP/SSE or IPC)
                            ▼
               ┌────────────────────────┐
               │    MCP Tool Server     │  (internal/server, internal/registry)
               │ (Native & Plugin Tools)│
               └────────────────────────┘
```

### Key Components

| Concept | Role | Location |
|---|---|---|
| **Trigger Manager** | Evaluates event rules, cron schedules, and worker lifecycles | `internal/trigger/manager.go` |
| **Rule Engine** | Smart search AST (`and`, `or`, `not`, dot-notation field lookups, ops) | `internal/trigger/rule.go` |
| **Supervised Runner** | Subprocess execution, timeout management, stdout/stderr capture | `internal/trigger/runner.go` |
| **Event Bus** | Central transport for CloudEvents fan-out | `internal/supervisor/event_bus.go` |
| **Tool Registry & MCP** | Exposes native tools and plugins over MCP SSE and CLI | `internal/registry/`, `internal/server/` |
| **Audit Store** | SQLite database recording all script runs and tool calls | `internal/store/audit.go` |
| **Web UI** | Browser management interface at `/ui/` | `internal/webui/` |

### Removed Components

- **Starlark interpreter** — deleted (`internal/interpreter/` is gone).
- **YAML state machines & Actuators** — deleted. Workflows are external scripts.
- **Bubbletea TUI & Chat REPL** — deleted (`internal/tui/`, `clara chat`, `clara dashboard` removed).
- **HITL Approvals & Builder Mode** — deleted.

---

## CLI Surface

```bash
# Daemon Management
clara serve                         # Start the daemon in foreground
clara status                        # Report trigger counts, tool counts, and daemon status
clara agent {start,stop,status}     # macOS LaunchAgent management

# Triggers
clara trigger list                  # List all loaded triggers
clara trigger show <id>             # Show trigger definition and match rules
clara trigger run <id>              # Manually run a trigger script

# Execution Runs & Audit
clara run list [-n N]               # Inspect recent execution runs
clara run show <id>                 # View run details (stdout, stderr, exit code, event)

# Tools & MCP
clara tool list                     # List registered MCP tools
clara tool show <name>              # Show JSON schema for a tool
clara tool call <name> -a '<json>'  # Execute a tool directly
clara types gen [--out path]        # Auto-generate TypeScript definitions (.d.ts) for tools
clara mcp list                      # List external MCP servers

# Events
clara event emit --type=<t> -d '<json>'  # Emit a CloudEvent onto the event bus
clara event logs [-n N] [-f]             # Stream CloudEvents from the event bus
```

---

## Trigger Definition Schema

Triggers are defined in YAML files in `~/.config/clara/tasks/` (or paths in `task_dirs`):

```yaml
# Event Trigger Example
id: email-invoice-processor
name: Email Invoice Processor
description: Routes invoice emails to an automated TypeScript processing script
type: event
debounce: 500ms           # optional: wait for quiet period before running
throttle: 1s              # optional: rate limit consecutive executions
match:
  and:
    - field: type
      op: equals
      value: email.received
    - field: data.mailbox
      op: equals
      value: inbox
    - or:
        - field: data.subject
          op: contains
          value: Invoice
        - field: data.subject
          op: regex
          value: "(?i)receipt|bill"
action:
  exec: bun
  args: ["run", "scripts/process_invoice.ts"]
  pass_event: stdin       # stdin | env | arg | none
  timeout: 30s

---
# Worker Trigger Example
id: telegram-bridge-worker
name: Telegram Bridge Worker
description: Supervised long-running daemon process
type: worker
action:
  exec: python3 workers/telegram_bridge.py
  restart: always         # always | on_failure | never
  restart_delay: 5s
  max_restarts: 10
```

---

## Build / Lint / Test Commands

```bash
pnpm install        # install webui frontend dependencies
make build          # generate templ, build vite assets, and compile bin/clara
go build ./cmd/clara

make test           # go test ./... -timeout 60s
go test ./internal/trigger -run TestRule -v

make vet            # go vet ./...
make lint           # staticcheck ./...
make fmt            # golines -m 100 --base-formatter goimports -w ./...

go mod tidy         # after adding/removing dependencies
make install        # install as macOS LaunchAgent
```

All committed code must pass `go vet ./...` and `staticcheck ./...` with no warnings or errors.

---

## Project Structure

```
cmd/
  clara/            # Unified CLI binary (root, serve, agent, triggers, runs, tools, events)
  integrations/     # Native Go integration SENSOR plugins (go-plugin RPC)
    chrome/         # Browser automation bridge
    discord/        # Discord relay (via Eve)
    llm/            # LLM multiplexer (Gemini, Ollama)
    task/           # Task tracking
    tmux/           # Terminal multiplexer
    web/            # Web search
    webex/          # Webex relay (via Eve)
    zk/             # Zettelkasten/Obsidian vault
internal/
  trigger/          # Trigger Engine: rules AST, runner, trigger manager
  store/            # SQLite store (runs, tool audits, vec search)
  registry/         # Central tool registry with audit logging
  server/           # HTTP & MCP SSE server
  supervisor/       # EventBus and CloudEvent core types
  webui/            # Templ-based Web Management UI (/ui/)
    templ/          # Templ template components
    dist/           # Compiled Vite assets
  config/           # Config loader (~/.config/clara/config.yaml)
  ipc/              # Unix domain socket IPC protocol
  loghub/           # Central ring-buffer log hub
  ringbuf/          # Thread-safe circular buffer
```

---

## Code Style & Formatting

- **Line length:** 100 characters (`golines -m 100`).
- **Formatter:** `golines` + `goimports` (run `make fmt` before committing).
- Tabs for indentation, no semicolons, double quotes for strings.
- Struct field tags column-aligned within a struct.

**Import order** (three groups, blank line between):
```go
import (
    "context"
    "fmt"

    "github.com/cockroachdb/errors"
    "github.com/rs/zerolog"

    "github.com/brightpuddle/clara/internal/config"
)
```

**Naming:** `snake_case.go` files, `lowerCamelCase` unexported, `CamelCase` exported, `New<Type>(...)` constructors. Strongly-typed structs with JSON/YAML tags.

---

## Error Handling & Logging

```go
import "github.com/cockroachdb/errors"

return errors.Wrap(err, "failed to parse trigger rule")
return errors.Newf("invalid operator: %q", op)
```

- Always wrap errors with context using `errors.Wrap` or `errors.Newf`.
- Structured logging using `zerolog`:
```go
log.Info().Str("trigger_id", id).Str("event_type", ev.Type).Msg("trigger fired")
```
- Integration plugins must never write to stdout (reserved for go-plugin RPC framing); use stderr or `zerolog`.

---

## Concurrency & Safety

- Use `github.com/sourcegraph/conc` for goroutine pools with panic recovery.
- All long-running goroutines and processes must be bounded by a `context.Context`.
