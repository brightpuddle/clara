# opencode v2 API Spike

Date: 2026-11-04. Installed: opencode v2.2.5 on eve. Source: live service
(`opencode service status`, `opencode api ...`) and
`https://opencode.ai/v2/openapi.json`. Feeds ADR 0004 and the Cockpit design.

## Headline

opencode v2 is already client-server. A background service owns sessions,
permissions and tool execution, and the TUI is just a client of it. The
"switch TUI sessions in tmux to server sessions" step in the design is
therefore mostly unnecessary: sessions already live in the service. The
opencode adapter becomes an HTTP client of that service, with no tmux scraping.

## Verified

- Service discovery: `opencode service status` prints the URL
  (`http://127.0.0.1:49374` on eve, random port). `opencode api get /api/info`
  returns version, pid, hostname, `capabilities.persistentPty`.
- `GET /api/session` lists sessions with `id`, `parentID` (subagents),
  `agent`, `model`, `cost`, `tokens`, `outcome`, `time.{created,updated,busy,
  idle,viewed}`, `title`, `location.directory`. This session is listed.
- `GET /api/session/active` returns `{ses_...: {type: "running"}}`: a direct
  running/idle signal.
- `GET /api/session/{id}/message` returns typed messages
  (`type: assistant`, `content[]` of parts such as `reasoning`, text, tool
  calls). This is the structured history; no scraping.

## Relevant endpoints (from OpenAPI)

| Need | Endpoint |
|---|---|
| Spawn session | `POST /api/session` |
| Send to agent | `POST /api/session/{id}/prompt` (durably admits input; `resume` flag) |
| Queue while busy | `GET/PATCH/DELETE /api/session/{id}/inbox` |
| Interrupt | `POST /api/session/{id}/interrupt` |
| Running state | `GET /api/session/active` |
| Question to user | `GET /api/form`, `.../session/{id}/form`, `POST .../form/{formID}/reply` |
| Permissions (unused; agents are permissive) | `.../permission`, `.../reply` |
| Live events | `GET /api/event` (SSE) |
| Wait for completion | `POST /api/experimental/session/{id}/wait` |
| Diffs / VCS | `GET /api/session/{id}/diff`, `/api/vcs/*` |
| Pairing code for remote clients | `POST /api/pair`, `GET /auth/connect/{code}` |
| Voice (experimental) | `/api/experimental/voice/*` |
| Terminal fallback | `/api/pty/*`, `persistent-pty/*` (WebSocket connect) |
| Typed client | `@opencode/client` (TS), OpenAPI for codegen |

## Consequences for the design

1. **The event stream is volatile by contract.** `GET /api/event` says a slow
   consumer overflows and fails, and events during disconnection are missed.
   The Clara adapter must (a) persist every event to the Go event log
   immediately, and (b) on reconnect reconcile via
   `session.message.list` / `session.active`. This strongly validates ADR 0001.
2. **"Needs you" likely maps to Forms**, not permissions. Forms have `title`,
   `fields`, and a `Form.Reply` answer; `GET /api/form` lists pending ones.
   This matches the agent asking the user a question (as this conversation
   does). Needs confirming (see below).
3. **Prompting a busy agent is supported** via the inbox, which fits "send
   instructions to a long-lived agent as they come up".
4. **Eve and Sophia each run their own opencode service.** Clara adapters
   connect to the local service per host. Use `opencode serve --hostname
   --port` for a stable address if the random port is awkward.
5. **Overlap risk.** opencode has pairing codes and an experimental voice API,
   so it may grow its own remote clients. The Cockpit's value is unifying
   opencode and agy across hosts with one attention model, and Clara's own
   notification and log ownership. Keep the adapter thin so it can change.

## Verified by probe (spike/opencode_probe.py, spike/record_events.ts)

Run against the live service with `gemini-3.8-flash`. Fixture:
`spike/out/probe_session_events.jsonl`.

**Full loop works.** `POST /api/session` -> `POST .../prompt` -> agent calls its
question tool -> a Form appears -> `POST .../form/{id}/reply` -> agent finishes
("You prefer red.") -> session goes idle with `outcome: succeeded`.

**Questions are Forms.** `form.created` carries
`metadata.kind: "question"`, `metadata.tool.{messageID,id}`, and `fields[]`
(`key`, `title`, `description`, `type`, `options[{value,label,description}]`,
`custom: true` meaning free text allowed). Reply body is
`{"answer": {"<key>": "<value>"}}`. `form.replied` echoes the answer. This
maps one-to-one onto the Cockpit Attention (`options[]`, `allow_text`).

**Event vocabulary** (global stream, `GET /api/event`, typed client
`client.event.subscribe()`): `server.connected` (resync marker),
`session.created`, `session.inbox.enqueued/delivered`,
`session.execution.started/succeeded` (failure variant not yet observed),
`session.step.started/streamed/ended`, `session.reasoning.started/delta/ended`,
`session.text.started/delta/ended`, `session.tool.input.started/ended`,
`session.tool.called/progress/success`, `session.usage.updated` (cost, tokens),
`form.created`, `form.replied`, plus housekeeping (`project.updated`,
`provider.updated`, `mcp.status.changed`, `shell.created/exited`, ...).
Events carry `location.directory`. The stream is global across all sessions
and locations, including the session you are in.

**Durable per-session sequence.** Session-scoped events carry
`durable: {aggregateID: <sessionID>, seq, version}`. For the probe session
the sequence was gap-free, 0 to 20, from `session.created` to
`session.execution.succeeded`. Non-durable events (`form.*`,
`session.usage.updated`, `*.updated`) have no seq.

**Lossless catch-up exists.** `GET /api/experimental/session/{id}/log?after=<seq>&follow=true`
reads durable events after an exclusive sequence number and continues live.
Experimental, but it is exactly the per-session cursor the adapter needs.

**Transport notes.**
- Direct HTTP needs auth (401 without it). Use `Service.ensure()` and
  `Service.headers(endpoint)` from `@opencode/client/service` (Node/Bun), or
  `opencode api` for ad hoc calls.
- `opencode api` buffers; it does not stream SSE. Use the typed client.
- The TypeScript client is Bun-friendly. A Go adapter has to discover the
  endpoint and auth itself (see open items), or Clara can use a small Bun
  sidecar for opencode. Decide in the plan.

## Adapter design that follows

1. Subscribe to the global stream for liveness and discovery (`session.created`,
   `form.created`, `session.execution.*`).
2. For each tracked session keep `last_seq`. On start and on every reconnect
   (`server.connected`), read `/session/{id}/log?after=last_seq&follow=true`
   to fill gaps losslessly. Write each durable event into the Clara log with
   its `(host, sessionID, seq)` as an idempotency key.
3. Non-durable events (forms, usage) are reconciled on reconnect from
   `GET /api/session/{id}/form` and `session.get`.
4. Map to Cockpit events: `session.text.*` to `assistant_*`,
   `session.tool.*` to `tool_call/tool_result`, `form.created` to
   `attention_requested`, `form.replied` to `attention_resolved`,
   `session.execution.*` to `status_change`.

## Still open

- Failure and interrupt events (`session.execution.failed`?). Trigger one.
- A prompt sent via API appearing live in an attached TUI (expected, both are
  clients of one service).
- How to discover the endpoint and auth from Go: the registration file
  location, header format, and whether it rotates. Read the `Service` source in
  `@opencode/client` or the troubleshooting docs.
- Remote access: `opencode serve --hostname --port` auth, vs Tailscale-only
  binding. Also whether `/api/pair` is useful to us.
- Stability of `/api/experimental/session/{id}/log` across opencode releases.
- Do forms that remain unanswered survive a service restart?

## agy (not started)

Still to do: confirm hook payloads for `agy` 1.5.0 on a long-lived session,
whether a question to the user produces any hook or transcript signal, and
whether anything similar to opencode's service API exists (`agy --help`).
