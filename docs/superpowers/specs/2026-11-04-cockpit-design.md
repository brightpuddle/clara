# Clara Cockpit Design

Status: draft (exploratory, pre-spike). Terms are defined in `docs/GLOSSARY.md`.
Decisions are recorded in `docs/adr/`.

## 1. Vision

One purpose-built app for directing agents from anywhere: structured
conversations, reliable attention signals, and identical state on every device.

The goal is to stop being tied to a desk. Today Discord is the supervision
surface because it has native notifications, multi-channel view, dictation and
a mature mobile app. It is a poor fit because it is a sink with no knowledge of
agent state, and agent output is squeezed through tmux scraping. The Cockpit
replaces Discord for agent sessions with a native client on iPhone and Mac,
backed by a Clara-owned event log. Terminals and tmux become a runtime and a
fallback, not the interface.

### Usage patterns that drive the UX

- **Long-lived agents** kept open for big projects and fed instructions over
  time. Each is a persistent thread, like a DM.
- **Task agents** spawned for short jobs: spawn, delegate, review, dismiss.
  Needs a fast "new task" flow (host, project, agent, model).
- **Direction and feedback.** Agents run fully permissive. The user is
  interrupted only when an agent asks for guidance or a question. Text and
  dictation are the primary inputs. "Approval" therefore means *answer a
  question*, not *grant a permission*.
- **Mobility**: desk, yard, bike. Dictation and (later) hands-free voice matter.

### Non-goals

- Writing or embedding a terminal emulator in v1. A fallback terminal view may
  come later (SwiftTerm).
- A GPU-accelerated custom terminal.
- Multi-user support. One user, several of their own devices.
- Non-Apple clients.

## 2. Current state (survey of `main`, 2026-11-03)

- **Topology.** `eve` (primary, coordinator) and `sophia` (satellite laptop).
  Sophia opens a reverse SSH tunnel to Eve and registers as an MCP server
  (`clara mcp add laptop`); its tools appear on Eve as `laptop.*`.
- **API surface.** `internal/server`: `POST /mcp`, `GET /mcp/sse`, and
  `GET /events` (SSE firehose of plugin events, bearer token). `/events` has no
  cursor, no replay and no per-device state.
- **agy.** Spawned in a detached tmux session. Output via hooks
  (`UserPromptSubmit`, `Stop`) reading JSONL transcripts
  (`scripts/assistant/agy_stop_hook.ts`). Input via `tmux send-keys`.
- **opencode.** Interactive sessions are TUI-in-tmux, read by polling
  `tmux capture-pane` with a spinner regex and TUI-glyph stripping
  (`scripts/assistant/chat_router.ts`). Clara does not use opencode's server API.
- **Conversation state.** Lives in `assistant_state.db` owned by the TypeScript
  chat router, separate from Go's `clara.db`.
- **Notifications.** macOS (ClaraBridge), Discord, Webex. No APNs.
- **Questions to the user.** `notify.ask` with options and optional free text
  (see `2026-05-11-interactive-notifications-design.md`).

## 3. Architecture

```
iPhone / Mac app (SwiftUI)
        |  WSS + HTTPS, cursor-based sync, over Tailscale
        v
Eve: Clara cockpit service (Go)
  - Event log (SQLite): hosts, sessions, events, attention, device cursors
  - Attention state machine
  - APNs dispatcher (+ macOS notifications)
  - Agent adapters: opencode (server API), agy (hooks)
        ^
        |  events forwarded over the existing Sophia -> Eve tunnel
Sophia: same adapters run locally, push events to Eve tagged with host id
```

Principles:

1. **Structured events are the source of truth.** tmux is where agents run and a
   fallback for manual access, never the data source.
2. **One Go-owned event log on Eve.** The TS chat router becomes a consumer of
   it (as the Discord client adapter), not an owner of conversation state.
3. **The protocol is the product.** Clients are replaceable front ends.
4. **Phone and Mac connect only to Eve.** Sophia is never client-facing. It
   forwards events up; Eve stays correct when Sophia sleeps and presents it as
   "offline, last seen X".
5. **New API, not `/events`.** `/events` stays for automation/triggers.

### 3.1 Data model (initial)

```
Host      { id, name, online, last_seen }
Session   { id, host, agent: opencode|agy, dir, title,
            kind: longlived|task,
            state: running|idle|needs_you|failed|done,
            backing: { opencode_session_id | tmux_target } }
Event     { seq, session, ts, type, payload }     // append-only; seq = cursor
Attention { id, session, kind: question, options[], allow_text,
            answered_at, answer }
Device    { id, apns_token, cursors, revoked }
```

Event types: `user_message`, `assistant_delta`, `assistant_message`,
`tool_call`, `tool_result`, `diff`, `status_change`, `attention_requested`,
`attention_resolved`, `session_summary`.

Read state is per device and server-side, so reading on one device clears
badges on the others.

### 3.2 API (initial)

- `GET /v1/sync?cursor=` catch-up after offline.
- WebSocket for live events, with cursor on reconnect.
- `POST /v1/commands` (spawn, send, interrupt, answer, mark-read, kill) with
  client-generated idempotency keys.
- Device registration and revocation; bearer tokens, in addition to Tailscale.
- Wire schema is a single source file (JSON Schema or protobuf) from which Go
  and Swift types are generated.

### 3.3 Agent adapter interface

`spawn`, `send`, `interrupt`, `status`, `events`. Per agent:

- **opencode:** HTTP client of the v2 background service (see spike doc).
  prompts). Desktop access to the TUI via `opencode attach`. To be verified in
  the spike.
- **agy:** hooks plus JSONL transcripts for output; tmux `send-keys` for input.
  Turn-based events; mid-turn streaming and question detection are unknown.

### 3.4 Attention model

Small by design. Notify only on: `needs_you` (agent asked a question), `done`,
`failed`. Notifications carry the question's options as actions.

### 3.5 Clients

Native SwiftUI, shared package, iOS target first, then macOS. Located in
`swift/Cockpit/` in this repo. Requires Apple Developer Program ($99/yr) for
APNs, time-sensitive notifications and TestFlight. Dictation via system
keyboard first, then `SpeechAnalyzer`; Live Activities and App Intents later.

## 4. Open questions / risks

1. **agy questions.** How does agy surface "I need guidance" to hooks? If not
   detectable, fall back to turn-end heuristics or a tmux-aware path.
2. **opencode API fidelity.** Does it expose question/permission events usable
   for `needs_you`? Spike to confirm against the installed version.
3. **Migration of `chat_router.ts` behaviour** (reactions, `? question`,
   `/kill`, backfill) onto the event log. Do it feature by feature.
4. **Sophia offline UX** and event replay when it reconnects.
5. **Single point of failure:** Eve down means the Cockpit is down. Accepted;
   the app must say so clearly.
6. **Swift footprint** is currently only ClaraBridge.

## 5. Sequence

1. Spike: opencode server API and agy hook behaviour (throwaway scripts).
2. Go cockpit service: schema, SQLite log, sync API, replay/tail CLI, opencode
   adapter, agy hook ingestion.
3. Host federation (Sophia to Eve).
4. iOS app: inbox, thread view with markdown, composer with dictation, APNs.
5. macOS target.
6. Run alongside Discord, then retire Discord for agent sessions.
7. Extras: Live Activities, hands-free voice, App Intents, terminal fallback.
