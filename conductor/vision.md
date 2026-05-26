# Clara V2 — Vision & Implementation State

> **Read this at the start of every session.** It replaces the need to dig
> through git history or long conversations to understand where we are.

---

## What We Are Building

Clara is an autonomous personal assistant daemon. The core thesis:

- The system ingests **event streams** (messages, emails, logs, sensor data,
  human prompts) and routes them through an **LLM-backed Evaluator**.
- The Evaluator dispatches to compiled **Actuators** — standalone Go binaries
  loaded as gRPC subprocesses — or enters **Builder Mode** to write, compile,
  and load a new Actuator on the fly.
- Clara is self-modifying. When it encounters a task it cannot handle, it grows
  new capability without a human touching source code.
- Human oversight is preserved via a **HITL approval queue**: any action that
  exceeds declared capabilities blocks until the human decides via CLI.

This is an **exploratory branch ("antigravity")**. The goal is a working
proof-of-concept of the full self-modifying loop. If it succeeds, this
replaces the current main branch.

---

## What Has Been Removed

| Removed | Replacement |
|---|---|
| Starlark interpreter (`internal/interpreter/`) | Compiled Actuator binaries |
| YAML state machines | Actuator `Execute()` method |
| Intent-as-execution-unit | Actuator-as-execution-unit |
| Hot-reload of `.star` files | Builder Mode compiles and loads new binaries |
| `fsnotify` in daemon | Integrations emit CloudEvents instead |

The `orchestrator.Intent` type still exists as a thin metadata record but is
being phased out. Do not add new logic that depends on Intent workflow types.

---

## Architecture Snapshot

```
Integration Plugins (sensors only)
    │ CloudEvents
    ▼
Event Bus  →  Evaluator  →  [heuristic hit]  →  Actuator (gRPC subprocess)
                  │
                  └→  [LLM fallback]  →  invoke existing Actuator
                  │                      or
                  └→  [no match]  →  Builder Mode
                                         │ go build + LLM codegen loop
                                         ▼
                                     new Actuator binary → load → invoke
```

**Key files:**

| File | Purpose |
|---|---|
| `internal/supervisor/event.go` | `CloudEvent` struct |
| `internal/supervisor/event_bus.go` | Fan-out publish/subscribe |
| `internal/supervisor/evaluator.go` | Heuristic cache + LLM routing |
| `internal/supervisor/builder.go` | Sandboxed `go build` + LLM feedback loop |
| `internal/supervisor/cbac.go` | Capability-Based Access Control |
| `internal/supervisor/hitl.go` | `ApprovalStore` — blocking HITL queue |
| `internal/loghub/hub.go` | Ring-buffer log hub (event/evaluator/actuator) |
| `internal/ringbuf/ringbuf.go` | Thread-safe circular buffer with Subscribe |
| `internal/ipc/ipc.go` | Wire protocol: `Request`, `StreamRequest`, methods |
| `internal/ipc/server.go` | Unix socket server; routes streaming vs. regular |
| `cmd/clara/observe.go` | `daemonHandler` + CLI: event/evaluator/actuator cmds |
| `cmd/clara/approvals.go` | CLI: `clara approvals` + `clara request` |
| `pkg/sdk/` | Actuator SDK (interface, types) — **not yet created** |

---

## Implementation State

### Done ✓

- **Event Bus** (`internal/supervisor/event_bus.go`) — CloudEvent fan-out,
  both legacy `Event` and `CloudEvent` publish paths.
- **CloudEvent type** (`internal/supervisor/event.go`) — standard schema.
- **Evaluator skeleton** (`internal/supervisor/evaluator.go`) — heuristic
  cache, LLM fallback dispatch, Builder Mode trigger. `executeActuator` is a
  stub (logs + returns nil).
- **Builder** (`internal/supervisor/builder.go`) — sandboxed `go build` +
  `go test` loop, cross-device copy, `CommitToGit`. The go.mod written into
  the sandbox needs refinement (module path and replace directives).
- **CBAC** (`internal/supervisor/cbac.go`) — capability grant/authorize with
  wildcard resource matching.
- **HITL** (`internal/supervisor/hitl.go`) — `ApprovalStore` (submit/list/
  get/decide), `ActiveRouter` (swappable prompters).
- **BoltDB state** (`internal/store/bolt.go`) — per-actuator key-value store.
- **Ring buffer** (`internal/ringbuf/ringbuf.go`) — thread-safe, fixed
  capacity, `Subscribe(ctx, tail)` for history + live stream.
- **Log hub** (`internal/loghub/hub.go`) — typed `PushEvent`, `PushEvaluator`,
  `PushActuator`; per-actuator sub-buffers.
- **IPC streaming** (`internal/ipc/`) — `StreamRequest` wire type; server
  routes streaming methods to `HandleStream`; `rawWriter` auto-cancels on
  disconnect.
- **CLI observability** (`cmd/clara/observe.go`) — `clara event logs`,
  `clara evaluator logs`, `clara actuator list/logs/run` with `--tail`/
  `--follow`/`--type`/`--source`/`--payload` flags; `daemonHandler` struct
  wires the hub into IPC.
- **CLI approvals + request** (`cmd/clara/approvals.go`) — `clara approvals
  list/show/decide`, `clara request "<prompt>"`.
- **Supervisor extensions** — `ActuatorInfos()`, `EmitPromptEvent()`,
  `RunActuator()` added to `internal/supervisor/supervisor.go`.
- **Build is clean** — `go build ./...`, `go vet ./...`, `go test ./...` all
  pass.

### Stub / Incomplete ✗

- **`executeActuator`** in `evaluator.go:132` — returns nil. Needs to actually
  launch the compiled binary as a go-plugin gRPC subprocess and call
  `Execute(ctx, event)`.
- **`pkg/sdk/`** — Actuator SDK package does not exist yet. Must define the
  `Actuator` interface, `ActuatorManifest`, `Capability`, `Event`, `Result`,
  `State`, and the `Serve(impl Actuator)` bootstrap. This is the contract the
  Builder's LLM codegen will produce against.
- **Evaluator ↔ Builder ↔ Supervisor wiring** — `Evaluator` and `Builder` are
  instantiated but not connected to the live event stream in `serve.go`. The
  daemon does not yet subscribe the Evaluator to CloudEvents from the bus.
- **LLM integration** — `Evaluator.llm` is an interface (`LLMClient`) with no
  real implementation wired in. Needs a concrete adapter to the `llm`
  integration plugin (or direct API call).
- **Integration plugins as sensors** — existing plugins (webex, discord, fs,
  shell, etc.) still use the legacy `contract.Integration` / tool-call model.
  They need to be adapted to emit `CloudEvents` to the bus instead of
  registering tools into the registry.
- **`builder.go` go.mod** — the sandbox go.mod hardcodes a `require
  github.com/brightpuddle/clara v2.0.0` which will fail. Needs a `replace`
  directive pointing at the local module, or the SDK should be published.
- **`intent.go` CLI** — still present and functional for legacy compatibility
  during the transition. Will be removed once Actuators are the sole unit of
  execution.
- **`loghub` not yet published** — the daemon creates a `loghub.Hub` but
  nothing calls `hub.PushEvent`, `hub.PushEvaluator`, or `hub.PushActuator`
  yet. The ring buffers are empty.
- **`pkg/sdk/` Actuator contract** — `pluginLoader` in `cmd/clara/plugins.go`
  still loads legacy `contract.Integration` plugins. A parallel loader for
  Actuator binaries (using the SDK contract) does not exist yet.

---

## Next Session Priorities

Work through these in order. Each is a discrete, completable unit:

### Priority 1 — `pkg/sdk`: Define the Actuator contract

Create `pkg/sdk/actuator.go` with the full interface. This unblocks everything
downstream.

```go
package sdk

type Actuator interface {
    Manifest() ActuatorManifest
    Execute(ctx context.Context, event Event) (Result, error)
}
// + ActuatorManifest, Capability, Event, Result, State, Serve(impl Actuator)
```

`Serve()` wires into `hashicorp/go-plugin` as a gRPC plugin (parallel to how
`pkg/contract` works for integration sensors). Define the protobuf or use
`plugin.Plugin` with a `GRPCServer`/`GRPCClient` impl.

### Priority 2 — Wire the Evaluator into the Event Bus

In `cmd/clara/serve.go`, after the daemon starts, subscribe the `Evaluator`
to `CloudEvents` from the bus. Every CloudEvent should call
`evaluator.OnEvent(ctx, ce)` in a bounded goroutine pool.

```go
// In runDaemonServices or a new startEvaluator hook:
sub := eventBus.SubscribeCloud()
pool.Go(func() {
    for ce := range sub {
        if err := evaluator.OnEvent(ctx, ce); err != nil {
            log.Error().Err(err).Msg("evaluator error")
        }
    }
})
```

### Priority 3 — Implement `executeActuator`

Fill in `evaluator.go:executeActuator`. Look up the binary path for
`actuatorID`, launch it as a go-plugin gRPC client using the `pkg/sdk`
contract, call `Execute(ctx, sdkEvent)`, and handle the `Result` (retry,
escalate, or log success).

### Priority 4 — Publish to the log hub

Thread `*loghub.Hub` into the Evaluator and Builder so their decisions appear
in `clara evaluator logs`. Push CloudEvents from the bus into `hub.Event` so
`clara event logs` shows live traffic.

### Priority 5 — Concrete LLM adapter

Implement `LLMClient` using the existing `llm` integration plugin (or a direct
API call). The system prompt for `AnalyzeEvent` must include the `pkg/sdk`
source verbatim so the LLM understands the compilation target.

### Priority 6 — Builder go.mod fix

The sandbox `go.mod` needs a `replace github.com/brightpuddle/clara =>
<local-path>` directive so generated actuators can import `pkg/sdk` without a
published module.

---

## Design Decisions Already Made (Do Not Re-Debate)

- **Unix socket IPC, not gRPC, for CLI↔Daemon.** gRPC is only at the
  Actuator boundary.
- **Line-delimited JSON** for streaming (compatible with `jq`).
- **Ring buffers of 1000 entries** per stream; 90-day SQLite TTL pruner.
- **CBAC via manifest declaration** — actuators declare capabilities; anything
  undeclared blocks and raises HITL.
- **Builder produces standalone binaries** in `~/.local/share/clara/bin/`;
  loaded as go-plugin subprocesses (not in-process plugins).
- **No VM, no sandbox OS** — native macOS compilation for speed.
- **Per-actuator BoltDB** for private key-value state (not shared SQLite).
