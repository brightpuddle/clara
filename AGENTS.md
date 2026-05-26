# Clara — Agent Instructions

## Project Overview

**Clara** (`github.com/brightpuddle/clara`) is an autonomous, self-monitoring
personal assistant daemon. It is a background process written in Go that:

1. Ingests **event streams** from multiple sources (Webex, email, logs, CLI
   prompts, system sensors, its own errors, etc.) via a central **Event Bus**.
2. Routes events through an **Evaluator** — a fast-path heuristic cache backed
   by an LLM fallback — to determine what action to take.
3. Executes **Actuators** — self-contained compiled Go binaries that implement
   a typed SDK interface and are loaded as `hashicorp/go-plugin` gRPC
   subprocesses. Actuators are the _only_ unit of execution.
4. Self-modifies: when no Actuator exists for an event, the Evaluator enters
   **Builder Mode**, generates Go source code, compiles it, and loads the new
   Actuator without restarting.
5. Exposes a `cobra`-based CLI for daemon control, observability, and
   human-in-the-loop approvals.

> **This is an exploratory branch (antigravity).** The goal is a fully
> functional proof-of-concept of the autonomous self-modifying model before it
> becomes the main branch. The architectural direction is set; individual
> sessions drive incremental implementation forward.
>
> For a complete picture of the vision and current implementation state, read
> [`conductor/vision.md`](conductor/vision.md) before starting any task.

---

## Core Architecture

```
External World
    │
    ▼
┌──────────────────────────────────────────────────────┐
│  Integration Plugins  (go-plugin gRPC subprocesses)  │
│  webex / discord / email / fs / shell / sensors ...  │
│                                                      │
│  Role: SENSORS ONLY — emit CloudEvents, no logic.    │
└──────────────────────────┬───────────────────────────┘
                           │  CloudEvents
                           ▼
                    ┌─────────────┐
                    │  Event Bus  │  (internal/supervisor/event_bus.go)
                    └──────┬──────┘
                           │
                           ▼
                    ┌─────────────┐
                    │  Evaluator  │  (internal/supervisor/evaluator.go)
                    │             │
                    │  1. Fast-path heuristic cache
                    │  2. LLM fallback (AnalyzeEvent)
                    │  3. Builder Mode (no match found)
                    └──────┬──────┘
                           │
              ┌────────────┴────────────┐
              │  invoke                 │  build
              ▼                         ▼
     ┌─────────────────┐      ┌─────────────────┐
     │    Actuator     │      │    Builder      │
     │  (go-plugin     │      │  (compiler +    │
     │   gRPC binary)  │      │   LLM codegen)  │
     └─────────────────┘      └────────┬────────┘
                                       │ compiled binary
                                       ▼
                              ┌─────────────────┐
                              │    Actuator     │
                              │  (newly loaded) │
                              └─────────────────┘
```

### Key Distinctions

| Concept | Role | Location |
|---|---|---|
| **Integration Plugin** | Sensor: ingest external signals, emit CloudEvents | `cmd/integrations/<name>/` |
| **Actuator** | Executor: respond to events with real-world actions | `~/.local/share/clara/bin/` or `pkg/sdk/` |
| **Evaluator** | Brain: route events to actuators or trigger build | `internal/supervisor/evaluator.go` |
| **Builder** | Compiler: LLM codegen + `go build` feedback loop | `internal/supervisor/builder.go` |
| **Event Bus** | Transport: fan-out CloudEvents to subscribers | `internal/supervisor/event_bus.go` |
| **HITL / ApprovalStore** | Safety: block and queue requests for human decision | `internal/supervisor/hitl.go` |

### What Has Been Removed

- **Starlark interpreter** — deleted entirely (`internal/interpreter/` is gone).
- **YAML state machines** — deleted. No state machine transitions exist.
- **"Intent" as execution unit** — the `orchestrator.Intent` type still exists
  as a thin metadata record used during the transition, but it is being phased
  out. New code should not add logic that depends on Intent workflow types.
  Actuators replace intents as the unit of execution.

---

## CLI Surface

```
clara serve                         # Start the daemon
clara agent {start,stop,status}     # LaunchAgent management

clara event logs [-n N] [-f]        # Stream CloudEvents from the event bus
  [--type=<t>] [--source=<s>]

clara evaluator logs [-n N] [-f]    # Stream evaluator decisions (heuristics, LLM, builder)

clara actuator list                 # List loaded actuator binaries
clara actuator logs <id> [-n N] [-f]
clara actuator run <id> [--payload=<json>] [-f]

clara approvals list                # List blocked HITL approval requests
clara approvals show <id>           # Show context + options for a request
clara approvals decide <id> <N>     # Submit decision (1-based option index)

clara request "<prompt>"            # Dispatch a natural-language prompt as a CloudEvent
```

### IPC Wire Protocol

CLI ↔ Daemon communicates over a Unix domain socket with two modes:

- **Request/Response:** single JSON request, single JSON response (`ipc.Request`
  / `ipc.Response`).
- **Streaming:** `ipc.StreamRequest` sent once; daemon streams
  newline-delimited JSON entries until client disconnects or context cancels.
  Backed by `internal/ringbuf` (ring buffers, 1000 entries each).

Log hub (`internal/loghub`) publishes to three ring buffers: `Event`,
`Evaluator`, `Actuator` (plus per-actuator sub-buffers).

---

## Actuator SDK

Every actuator is a standalone Go binary that calls `sdk.Serve()`. The daemon
loads it as a `hashicorp/go-plugin` gRPC subprocess.

```go
// pkg/sdk/actuator.go (to be created)
type Actuator interface {
    Manifest() ActuatorManifest      // identity + capability declarations
    Execute(ctx context.Context, event Event) (Result, error)
}

type ActuatorManifest struct {
    ID           string       `json:"id"`
    Description  string       `json:"description"`
    Capabilities []Capability `json:"capabilities"` // CBAC enforcement
}
```

Capability-Based Access Control (`internal/supervisor/cbac.go`) enforces
declared capabilities. Undeclared resource access is blocked and triggers a
HITL approval request.

---

## Build / Lint / Test Commands

```bash
make build          # builds clara and all plugins in ./bin/
go build ./cmd/clara

make test           # go test ./... -timeout 60s
go test ./internal/supervisor -run TestName -v

make vet            # go vet ./...
make lint           # staticcheck ./...
make fmt            # golines -m 100 --base-formatter goimports -w ./...

go mod tidy         # after adding/removing dependencies
make bridge         # build Swift gRPC bridge (macOS only)
make install        # install as macOS LaunchAgent
```

All committed code must pass `go vet ./...` and `staticcheck ./...` with no
warnings or errors.

---

## Project Structure

```
cmd/
  clara/
    main.go         # cobra root, global cfg var, shared helpers
    serve.go        # daemon: plugin loader, event bus, evaluator wiring
    agent.go        # clara agent {start,stop,status,logs}
    observe.go      # clara event/evaluator/actuator commands + daemonHandler
    approvals.go    # clara approvals + clara request
    plugins.go      # pluginLoader: integration plugin discovery & loading
    intent.go       # legacy intent CLI (being phased out)
    tool.go         # clara tool {list,show,call}
  integrations/     # Native Go integration SENSOR plugins (go-plugin RPC)
    fs/             # Filesystem events
    llm/            # LLM multiplexer (Gemini, Ollama, etc.)
    shell/          # Local shell execution
    web/            # Web search
    chrome/         # Browser automation
    zk/             # Zettelkasten/Obsidian vault
    discord/        # Discord relay (via Eve)
    webex/          # Webex relay (via Eve)
internal/
  supervisor/       # Core engine
    event.go        # CloudEvent type
    event_bus.go    # Event, EventBus (publish/subscribe)
    evaluator.go    # Evaluator: heuristic cache + LLM routing + Builder trigger
    builder.go      # Builder: sandboxed go build + LLM feedback loop
    cbac.go         # Capability-Based Access Control
    hitl.go         # ApprovalStore + ActiveRouter (HITL blocking queue)
    supervisor.go   # Supervisor: manages actuator lifecycle
    schedule.go     # Scheduling (cron-like triggers)
  loghub/           # Central ring-buffer hub for observability streams
  ringbuf/          # Thread-safe fixed-capacity circular buffer
  ipc/              # Unix-socket IPC protocol (Request, StreamRequest, etc.)
  config/           # Config loader (~/.config/clara/config.yaml)
  store/            # SQLite persistence (runs, heuristics, evaluator memory)
  orchestrator/     # Legacy Intent/State types (being phased out)
  registry/         # Tool registry (used by legacy integrations)
  intentlog/        # Append-only run event log (legacy, will be superseded)
pkg/
  contract/         # go-plugin RPC/gRPC contracts (existing integrations)
  sdk/              # Actuator SDK interface (to be built)
conductor/
  vision.md         # ← READ THIS FIRST in new sessions
swift/              # Standalone Swift gRPC bridge (macOS)
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

**Naming:** `snake_case.go` files, `lowerCamelCase` unexported, `CamelCase`
exported, `New<Type>(...)` constructors.

Use `any` not `interface{}`. Strongly-typed structs with JSON tags for
serialization. Avoid heavy reflection.

---

## Error Handling

```go
import "github.com/cockroachdb/errors"

return errors.Wrap(err, "failed to load config")
return errors.Newf("unsupported mode: %q", mode)
```

Never silently swallow errors. Use `errors.Is`/`errors.As` for inspection.

---

## Logging

```go
log.Info().Str("actuator_id", id).Str("event_type", ev.Type).Msg("dispatching")
log.Error().Err(err).Str("actuator", id).Msg("execution failed")
```

- **Integration plugins must never write to stdout** — reserved for go-plugin
  RPC framing. Use stderr for diagnostics.
- Daemon logs to `~/.local/share/clara/clara.log` by default.

---

## Concurrency

- Use `github.com/sourcegraph/conc` for goroutine pools with panic recovery.
- All goroutines must be bounded by a `context.Context`. No fire-and-forget.

---

## Testing

- Standard `testing` package. Table-driven tests preferred.
- No network or external service access in tests. Use interfaces and test
  doubles.
- Focus: `internal/supervisor/`, `internal/config/`, `internal/ringbuf/`,
  `internal/store/`.

---

## Dependencies

| Purpose | Library |
|---|---|
| Structured logging | `github.com/rs/zerolog` |
| Error handling | `github.com/cockroachdb/errors` |
| SQLite (CGO-free) | `github.com/ncruces/go-sqlite3` |
| Actuator plugins (gRPC) | `github.com/hashicorp/go-plugin` |
| CLI | `github.com/spf13/cobra` |
| Tool spec (legacy integrations) | `github.com/mark3labs/mcp-go` |
| Structured concurrency | `github.com/sourcegraph/conc` |
| YAML parsing | `gopkg.in/yaml.v3` |
| BoltDB (per-actuator state) | `go.etcd.io/bbolt` |

Removed: `go.starlark.net/starlark`, `github.com/expr-lang/expr`,
`github.com/fsnotify/fsnotify` (no longer needed at the daemon level).

---

## Eve Relay Server

Discord and Webex integrations communicate via the **Eve relay server**
(`~/src/eve/main`, `github.com/brightpuddle/eve`) over HTTPS with a shared
bearer secret. Config lives in `~/.config/eve/config.yaml`. See
`~/src/eve/main/AGENTS.md` for Eve's conventions.

```yaml
# ~/.config/clara/config.yaml
integrations:
  discord:
    eve_url: "https://eve.brightpuddle.com"
    secret: "<shared bearer secret>"
    machine: "<this machine's name>"
  webex:
    eve_url: "https://eve.brightpuddle.com"
    secret: "<shared bearer secret>"
    machine: "<this machine's name>"
```
