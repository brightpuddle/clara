# Cockpit Event Schema and Adapter Interface

Status: draft. Builds on `2026-11-04-cockpit-design.md` and the evidence in
`2026-11-04-opencode-v2-spike.md`. Terms are in `docs/GLOSSARY.md`; decisions
are ADRs 0001 to 0005.

## 0. Layering and Clara integration

Two layers, delivered in this order:

- **Agent core** (Stage 1, package `internal/agents`): adapters, `SessionRunner`,
  host log, and the Clara-facing surface. This is host mode only and has no
  client API.
- **Hub and client API** (Stages 2 and 3): aggregation across hosts, `/v1`
  endpoints, devices, APNs.

The core plugs into Clara the way existing capabilities do:

- A builtin `internal/builtin/agent` registers MCP tools in an `agent` namespace
  (`agent.session.create/list/get/close/resume/discover`, `agent.prompt.send`,
  `agent.prompt.cancel`, `agent.turn.interrupt`, `agent.events.read`).
- Logged events are also published to the event bus as CloudEvents
  (`agent.session.created`, `agent.message`, `agent.turn.started`,
  `agent.turn.ended`, `agent.attention.requested`, `agent.session.status`,
  and so on; data = the event payload plus `host`, `session`, `host_seq`).
- The bus is volatile fan-out. The log is the durable record. Consumers (the TS
  chat router, later the hub) catch up with `agent.events.read(after=host_seq)`.
- On a satellite the same tools appear as `laptop.agent.*` through the existing
  remote MCP mechanism, so the Stage 2 puller needs no new transport.
- A `clara agent` CLI (list, new, send, tail, cancel) drives the core without
  any chat client, for development and validation.

## 1. Shape of the system

Every host that runs agents runs the same Cockpit core in **host mode**:
adapters plus a small local durable log. Eve additionally runs **hub mode**:
it aggregates the logs of all hosts (itself included), serves clients, and
sends push notifications.

```
iPhone / Mac app
      | WSS + HTTPS /v1 (cursor-based)
      v
Eve  [hub]  global log (seq) <-- ingest --+-- Eve [host]: adapters, local log
      |                                    |
      |  pull by host-local cursor         |
      +---- over existing tunnel (127.0.0.1:14444) --> Sophia [host]: adapters, local log
```

Why the hub **pulls**: Sophia's tunnel is outbound from Sophia, so Eve can reach
Sophia but not the other way round. Pulling by cursor also makes Sophia sleeping
harmless: events accumulate in its local log and Eve catches up on reconnect
(this is the same cursor mechanism clients use).

Hub ingest assigns the global `seq` and de-duplicates on `(host, host_seq)`.

## 2. Event envelope

```
Event {
  seq        int64      // global, assigned by the hub; client cursor
  host_seq   int64      // per-host, assigned by the host log; pull cursor
  id         string     // ULID
  host       string     // "eve" | "sophia"
  session    string     // Clara session id; empty for host events
  ts         time
  type       string     // see section 3
  payload    json
}
```

Rules:
- Append-only. Corrections are new events.
- **Streaming deltas are not logged.** `message.delta` events go over the live
  WebSocket only. The log stores the finished `message.assistant`. A client that
  connects mid-turn receives the session's in-progress text buffer as part of
  its snapshot (section 6), then live deltas.
- Payloads carry summaries, not raw tool I/O. Large outputs are stored by
  reference (`blob_id`) and fetched on demand.

## 3. Event types

| Type | Payload (key fields) | Notes |
|---|---|---|
| `host.status` | `online`, `last_seen` | hub-generated |
| `session.created` | `agent`, `dir`, `title`, `kind: longlived/task`, `model`, `origin: managed/observed`, `native_id?` | |
| `session.updated` | `title?`, `model?`, `kind?`, `native_id?` | `native_id` is the agent's own id, needed to resume |
| `session.status` | `state`, `reason?`, `blocked?{kind, resets_at}` | see 4 |
| `message.user` | `message_id`, `text`, `attachments[]`, `origin: device/terminal/discord`, `prompt_id?` | |
| `message.assistant` | `message_id`, `turn_id`, `text` | final text only |
| `message.delta` | `message_id`, `text` | **live only, not logged** |
| `turn.started` | `turn_id`, `prompt_id` | |
| `turn.ended` | `turn_id`, `stop_reason: end_turn/cancelled/interrupted/error`, `usage?`, `tools_summary` | `tools_summary` = counts and last few tool titles |
| `tool.call` | `call_id`, `turn_id`, `name`, `kind`, `title`, `locations[]`, `status` | |
| `tool.update` | `call_id`, `status`, `output_summary?`, `blob_id?` | |
| `prompt.queued` | `prompt_id`, `text`, `position`, `origin` | client-visible queue |
| `prompt.sent` / `prompt.cancelled` | `prompt_id` | |
| `attention.requested` | `attention_id`, `kind: question/turn_ended`, `title`, `options[]`, `allow_text`, `native?{form_id}` | |
| `attention.resolved` | `attention_id`, `by`, `answer?` | `by`: device id, terminal, auto |
| `usage.updated` | `cost?`, `tokens` | |

## 4. Session state and attention

States: `starting`, `idle`, `running`, `blocked`, `failed`, `closed`.

- `running`: a turn is active.
- `idle`: no active turn, prompt queue empty.
- `blocked`: the agent cannot proceed for an external reason (quota, auth,
  rate limit). Carries `reason` and, if known, `resets_at`.
- `failed`: the adapter or agent process died or errored.

**Attention** answers "what needs me?". Open attentions drive the inbox.

- `question` (structured, from opencode forms): has `options[]` and `allow_text`.
  Resolved by `attention.answer`.
- `turn_ended` (agy and any agent without structured questions): created when a
  turn ends with `end_turn` and the prompt queue is empty. It means "your move".
  Auto-resolved when a new `message.user` or `turn.started` arrives from
  anywhere.

Inbox order: open attentions (oldest first), then `blocked`/`failed`, then
`running`, then `idle`. Notifications fire on: new `attention.requested`,
entering `blocked`, entering `failed`. A `turn.ended` that produced an attention
is not notified twice.

Quick replies: a `turn_ended` attention may carry `options[]` parsed best effort
from numbered lists in the final text (`[1] Red`, `1. Red`). The UI treats them
as optional chips, never as a requirement.

## 5. Commands (client to hub)

All commands carry a client-generated `command_id` (idempotency key; the hub
remembers results for a retention window) and a device id.

| Command | Effect |
|---|---|
| `session.create {host, agent, dir, model?, kind, prompt?}` | starts a managed session |
| `session.close {session}` | stops the agent process, keeps history |
| `session.resume {host, agent, native_id}` | adopts an existing agent conversation |
| `prompt.send {session, text, attachments?}` | enqueue a prompt |
| `prompt.cancel {session, prompt_id}` | remove a queued prompt |
| `turn.interrupt {session}` | cancel the running turn |
| `attention.answer {attention_id, answer}` | answers a question or replies |
| `session.mark_read {session, upto_seq}` | per-device read cursor |
| `device.register / device.revoke` | APNs token, credentials |

`attention.answer` on a `turn_ended` attention is equivalent to `prompt.send`.

## 6. Sync API (hub)

- `GET /v1/sync?after=<seq>&limit=` returns logged events after the cursor.
  If the cursor is older than retention or unknown, the response is
  `{reset: true}` and the client calls `GET /v1/snapshot`.
- `GET /v1/snapshot` returns: hosts, sessions (state, title, last event seq,
  open attentions, queue contents), per-device read cursors, and in-progress
  text buffers for running turns.
- `WS /v1/stream?after=<seq>` sends logged events, then live events
  including `message.delta`.
- `POST /v1/commands` takes a command envelope, returns the result.

Host mode is pulled by the hub through the `agent.events.read` tool over the
existing tunnel (Stage 2), with `host_seq` as cursor. A dedicated host HTTP
endpoint is optional later.

## 7. Adapter interface

Go sketch, in `internal/agents/adapter`:

```go
// Adapter creates and discovers agent sessions for one agent type on one host.
type Adapter interface {
    Name() string                                    // "agy", "opencode"
    Open(ctx context.Context, spec OpenSpec) (AgentSession, error) // new or resume
    List(ctx context.Context) ([]NativeSession, error)             // resumable
}

type OpenSpec struct {
    Dir      string
    Model    string
    ResumeID string // native session id; empty for new
}

// AgentSession is one live conversation. It handles ONE prompt at a time;
// queueing lives above it in the core SessionRunner.
type AgentSession interface {
    NativeID() string
    Prompt(ctx context.Context, p Prompt) error // returns after the turn is accepted
    Cancel(ctx context.Context) error           // session/cancel
    Events() <-chan AgentEvent                  // normalized, closed on exit
    Close(ctx context.Context) error
}

type AgentEvent interface{ agentEvent() }
// MessageDelta, MessageDone, ToolCall, ToolUpdate, TurnEnded{StopReason, Usage},
// Question{Options, AllowText, NativeID}, Blocked{Kind, ResetsAt}, UsageUpdate,
// Exited{Err}
```

**ACP adapter** (ADR 0005) implements this once, parameterised by a quirk set:

```go
type Quirks struct {
    SetModel      func(ctx context.Context, c RPC, session, model string) error // agy: session/set_model; opencode: session/set_config_option
    DetectBlocked func(text string) *agents.Blocked                              // agy quota text
}
```

There is no early-prompt-response quirk: `Prompt` returns `ErrBusy` while a prompt
is unanswered, so a second prompt is never sent and agy's early response cannot
occur. `Open`'s ctx bounds the session lifetime. Agent requests
(`session/request_permission`) are auto-approved; unknown methods get -32601.

Turn tracking inside the adapter uses the rule from the spike: a turn is over only
when the response to the **most recent** prompt arrives. Replayed history from
`session/load` is dropped until the load response returns.

**Core SessionRunner** (agent-agnostic):
- owns the prompt queue and emits `prompt.*` events,
- feeds one prompt at a time to the adapter,
- converts `AgentEvent`s into logged `Event`s and manages session state and
  attention,
- on adapter `Exited`, marks any open turn `interrupted` (never re-sends),
  sets state `failed`, and lets the supervisor restart and `Open` with `ResumeID`.

**Runner behaviour (`internal/agents/runner`):** one goroutine per session owns
all state; commands go through it. Prompts are queued and sent one at a time. An
interrupt cancels the running turn and leaves the queue; the next prompt runs.
`blocked` holds the queue (no automatic retry); a new user prompt retries from the
head. Cancelled and interrupted turns raise no attention; `end_turn` with an
empty queue raises `turn_ended`; a prompt error raises `turn_ended` titled
"Turn failed: ...". The restart count resets after the agent stays up for
`StableAfter`; after `MaxRestarts` consecutive failures the session stays
`failed` and keeps queueing. The queue is in memory; rebuilding it from the
projection after a core restart is part of 1.4.

**Process supervision (v1):** one `agy acp` / `opencode acp` process per managed
session, started by the host core. Restarts use exponential backoff with a cap.
Revisit one-process-per-agent if process count matters. Managed processes should
survive a core restart only if the supervisor is split out; v1 accepts that a
core restart interrupts running turns and resumes sessions idle.

**Observed sessions** (hand-started terminal agents via hooks and tmux) are an
optional later adapter with `origin: observed`, no `Prompt` over ACP, and input
by tmux send-keys.

## 8. Storage

Host log table (`cockpit_events`): `host_seq` PK, `id`, `session`, `ts`, `type`,
`payload`. Hub log adds `seq` PK and `(host, host_seq)` unique. Other tables:
`sessions` (projection), `attentions`, `prompts` (queue), `devices`,
`device_cursors`, `commands` (idempotency). Projections are rebuilt from events,
so `sessions` can be dropped and replayed. Retention for events: unlimited
initially (single user); blobs pruned by age.

## 9. Open items

- Federation auth: reuses the existing MCP shared secret.
- How the TS router maps a chat channel to a managed session (channel topic
  `agent:<host>:<session>`), and per-channel opt-in to the new path during
  cutover.
- Coexistence with hand-started tmux agents (observed sessions) during cutover.
- Quick-reply parsing rules; start with `[n]` and `n.` lists.
- opencode native adapter (forms, durable log) as a later optimisation (ADR 0005).
- agy background-command behaviour (spike doc) and the `terminal` client capability.
- ACP `terminal` / `fs` client capabilities: advertise none in v1.
