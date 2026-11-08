# Agent API Spike: opencode v2 and agy (Antigravity CLI)

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

## Verified by probe (docs/spikes/opencode_probe.py, docs/spikes/record_events.ts)

Run against the live service with `gemini-3.8-flash`. Fixture:
`internal/agents/testdata/opencode/session_events.jsonl`.

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

## agy 1.5.0 findings (docs/spikes/agy_acp_probe.py, docs/spikes/agy_acp_resume.py)

**agy has a real structured interface: ACP.** `agy acp` is an Agent Client
Protocol stdio server (JSON-RPC 2.0, NDJSON). The hooks-plus-tmux path Clara
uses today is the weak option. Also present: `--print --output-format
stream-json` (one-shot/turn mode), `agy remote-control` (daemon), `agy
mic-serve` (remote microphone), `--remote-agent`. The last three are unexplored.

**ACP flow verified:** `initialize` -> `session/new {cwd, mcpServers}` ->
`session/set_model` (optional) -> `session/prompt` -> streamed
`session/update` notifications -> `{stopReason: "end_turn", usage}`.
Resume path: `session/list` (244 conversations incl. ones started in the TUI,
with `title`, `cwd`, `updatedAt`) and `session/load` (resumes and replays
history as updates).

**Update vocabulary seen:** `user_message_chunk`, `agent_message_chunk`,
`tool_call` / `tool_call_update` (`toolCallId`, `name`, `args`, `status`,
`kind` e.g. `edit`, `title`, `locations[]`, `rawInput`),
`available_commands_update`, `config_option_update`, `session_info_update`.
Final result carries token `usage`. `--dangerously-skip-permissions` means no
permission requests arrived (none handled beyond a stub).

**Gaps relative to opencode:**
1. **No structured question.** Asked to ask a question, agy emitted plain text
   (`Do you prefer red or blue?\n- [1] (Recommended) Red\n- [2] Blue`) and
   ended the turn with `end_turn`. A question and a "done" are the same
   protocol event.
2. **No event sequence numbers and no replay.** `session/load` replays the
   whole history without ids. The Clara log must be the only record, and the
   adapter must not re-ingest replayed history after a reload.
3. **Failures look like success.** On the default model the account's quota
   was exhausted, and agy reported it as an `agent_message_chunk` ("Individual
   quota reached... Resets in 22h38m") followed by `end_turn`. Quota appears
   to be per model; `gemini-3.8-flash-low` worked. The adapter must detect
   this by text and surface a `failed`/`blocked` state.
4. **Process ownership.** ACP is a child process of the client, not a shared
   background service like opencode. If Clara restarts mid-turn the agent
   process dies. Needs supervision and a resume-on-start policy.
5. **Two processes on one conversation is unsafe.** A managed (ACP) session
   and a TUI attached with `agy --conversation <id>` must not run at once.

**Hooks still fire** for ACP sessions: Clara's global Stop hook recorded rows in
`agy_hook_dedup` for the probe conversations. Disable or bind the Discord
hooks for managed sessions to avoid double posting.

## Decisions that follow

- **agy adapter: ACP, managed by Clara.** A `session` is an ACP session owned
  by a Clara-supervised `agy acp` process. This replaces tmux `send-keys` and
  hook scraping for Cockpit sessions.
- **Two modes.** *Managed* (ACP, full fidelity, Cockpit can send input) and
  *observed* (an agy TUI started by hand in tmux, visible only via
  existing hooks, input via tmux). Build managed first; observed is optional.
- **Attention for agy = end of turn.** Every `end_turn` on a managed session
  means the ball is in the user's court. Do not try to distinguish "question"
  from "done" at the protocol level. Optionally parse `[n] option` lists into
  quick-reply chips (best effort, never required).
- **Quota/limit state is first class.** Detect quota text, emit
  `status_change: blocked(quota, resets_at)`, and show it in the inbox. Worth
  doing for opencode providers too.
- **Per-agent differences are the adapter's job** (opencode: forms, durable
  seq; agy: end-of-turn, no seq). The Cockpit event schema must accommodate
  both, so `attention.kind` is `question` (structured, with options) or
  `turn_ended` (free reply).

## Still open (agy)

- Permission request shape without `--dangerously-skip-permissions` (not
  needed today; skip unless policy changes).
- Cancellation (`session/cancel`) and interrupting a running turn.
- Behaviour of `session/prompt` while a turn is running (queueing?).
- `agy remote-control` and `mic-serve`: what they actually do, and whether
  they overlap with the Cockpit or help with voice.
- Plan mode / `--mode`, and `--agent` selection through ACP.

## Follow-up: ACP as universal adapter (2026-11-04)

- `opencode acp` exists on v2.2.5. It reports `OpenCode 2.2.5`, supports
  `list/resume/close/delete/fork`, and `session/list` returned 18 sessions
  that match the service, including this conversation. Unverified: whether it
  shares *live* state (running turns) with the background service.
- agy conversation storage: one SQLite DB per conversation under
  `~/.gemini/antigravity-cli/conversations/<id>.db` (WAL). `steps` is keyed by
  integer `idx` (a usable cursor) but payloads are protobuf blobs with no
  documented schema. Not a content source; at best a cheap "did I miss steps"
  check. Do not depend on it.
- agy `remote-control` / `--remote-agent` are tied to Google's cloud with no
  API (per the user). Out of scope.
- Decision recorded in ADR 0005.

## ACP cancel and busy-session behaviour (docs/spikes/acp_cancel_busy.py)

Run against `agy acp` (gemini-3.8-flash-low) and `opencode acp`
(github-copilot/gemini-3.8-flash). Raw logs: `internal/agents/testdata/acp/*.jsonl`.

### Cancel: works identically on both
Send the `session/cancel` **notification** (`{sessionId}`, no id) while a prompt
is running. The in-flight `session/prompt` request returns
`{stopReason: "cancelled", usage}` within about a second. The running tool call
gets a `tool_call_update` with `status: failed`. The session stays usable: a
follow-up prompt succeeded on both agents.

### Busy session: the two agents differ
- **opencode** rejects a second `session/prompt` immediately with JSON-RPC
  error `-32603 "Session already has an active ACP prompt: <id>"`. The first
  turn is unaffected and completes normally.
- **agy** accepts it and queues it, but with a misleading response: the first
  prompt's request returned `{stopReason: "end_turn"}` (with no `usage`) the
  moment the second was sent, while the first turn's output kept streaming for
  about six more seconds. The second prompt's user message and reply then
  followed, and its response (with usage covering both) came last. So for agy
  the response to an earlier prompt does not mean its turn finished.

### Idle detection
For both agents, a session is idle only when the response to the **most recent**
prompt has arrived and no cancel is pending. Never use the first prompt's
response when another was sent after it.

### agy runs long commands in the background and ends the turn
Asked to run `sleep 20; echo finished-bg` and report the output, agy replied
"I have started the command ... as a background task. I will wait..." and ended
the turn after about 6 seconds. For the next 45 seconds, long after the command
had finished, it sent **no** further updates (no tool completion, no message).
Its promise to wait is not backed by anything: the user has to prompt again.
Possible causes: no `terminal` client capability was advertised in
`initialize`, or agy's ACP mode always backgrounds shell commands. Open item.
For attention, this means an agy `end_turn` can mean "done", "asking you", or
"left work running"; the UI should show recent tool calls with the turn so the
difference is visible.

### Other notes
- opencode over ACP does not support `session/set_model`
  (`-32601`). Model selection is in `configOptions` (`model`, `thought_level`,
  `mode`), presumably via `session/set_config_option`.
- agy exposes models via `session/new` `models`, and accepts
  `session/set_model`.

## Adapter rules that follow

1. **Client-side prompt queue per session.** The adapter sends one prompt at a
   time and sends the next only when the previous response arrives. This avoids
   opencode's error and agy's early-return quirk, and gives the UI a visible,
   editable, cancellable queue of pending messages.
2. **Cancel is a first-class command** (`session/cancel`), clearing nothing
   from the queue unless asked.
3. **Idle = latest prompt answered.** Track prompt ids; ignore early responses
   for superseded prompts.
4. **Attach tool-call summaries to every turn end**, so "left running"
   shows up.
5. **Per-agent model selection** goes through a small quirk table.

## B0 findings: endpoint discovery and auth (2026-11-07, opencode 2.2.6)

- **Discovery:** the background service writes `~/.local/state/opencode/service.json`
  (mode 0600) with `{id, version, url, pid, password}`. The URL is loopback
  (`http://127.0.0.1:<port>`, port varies per start). Clara reads this file, checks
  `pid` is alive, and re-reads on connection failure (the file is rewritten when the
  service restarts).
- **Auth:** HTTP Basic with username `opencode` and the `password` from the file.
  Bearer and empty-username Basic return 401. `GET /api/event` (SSE) returns
  `server.connected` then heartbeats, so reconnect handling can key off that event.
- **CLI equivalent:** `opencode api GET /api/session` uses the same service; handy for
  fixtures and manual probes.
- **Restart behaviour (isolated `serve` instance, tested 2026-11-07):**
  - The password is regenerated on every start; the old one returns 401. Clara must
    re-read `service.json` (or reconfigure) on reconnect, never cache credentials.
  - Sessions persist across a restart; **forms do not** (`GET /api/form` is empty after
    restart). A form pending at restart is lost, along with the blocked turn. The
    adapter should emit `attention_resolved(cancelled)` for forms it knew about that
    vanish after `server.connected`, and surface the session as interrupted.
- **Remote binding:** `opencode serve --hostname 0.0.0.0` enforces Basic auth on the LAN
  address (401 without, 200 with the password), so non-loopback is not open. The
  password is per-start, so a remote client cannot hold a static secret. `POST /api/pair`
  returns a 5-minute one-time code for `GET /auth/connect/{code}`. **Decision:** bind to
  loopback and reach it over Tailscale (tailscale serve or the ssh tunnel Clara already
  uses); use `/api/pair` only for `opencode web` on the phone.
- **Log endpoint:** the durable log lives at `/api/experimental/session/{id}/log`
  (`?after=seq`, SSE; emits `log.synced` with `seq` when caught up). It is under
  `experimental`, so it may change. Mitigation: pin the opencode version in Clara, add
  an opt-in `integration` contract test that exercises session create, form create,
  log catch-up, and `/api/event`, and run it on every opencode upgrade.
- **Useful API facts for B1:** `/openapi.json` (authenticated) is the machine-readable
  spec; forms can be created via `POST /api/session/{id}/form`, so adapter tests need
  no model; replies are `POST .../form/{formID}/reply {answer}` and
  `POST .../permission/{requestID}/reply`; `POST .../interrupt` cancels a turn.

B0 is closed.

## B1 probe: the log endpoint does not replay (2026-11-08, opencode 2.2.6)

Probed with fresh sessions on `github-copilot/gemini-3.8-flash` (one trivial prompt).

- `GET /api/experimental/session/{id}/log` returns **only** `log.synced {aggregateID, seq}`
  (the head seq) for every `after` value (0, 3, 15, 80, omitted) and for `follow=true|false`.
  A live `follow=true` tail opened before a prompt delivered no durable events either. The
  frame format is SSE `data: {json}`; `after=-1` is a 400. The earlier assumption that this
  endpoint gives lossless catch-up is **wrong for 2.2.6**. Do not build on it.
- What does work: `GET /api/session/{id}/message` returns the typed durable history
  (`user`, `assistant` with `content[]`, `idle` with `outcome`, ...), each with a stable
  message `id` and `time.{created,streamed,completed}`. Query: `limit`, `order=asc|desc`,
  opaque `cursor`, `type` filter.
- `GET /api/event` (SSE) still carries live durable events with `durable.seq`, so a gap is
  detectable per session even though the log cannot fill it.

Consequences for the B1 design:

1. Live path: `/api/event`, tracking `durable.seq` per session.
2. Catch-up (start, reconnect after `server.connected`, or a seq gap): list
   `/session/{id}/message?order=asc` and ingest messages not yet seen, keyed by message id
   (idempotent). Granularity is messages, not events: tool calls and text come from the
   assistant message `content[]`; deltas are live-only anyway.
3. Reconcile forms from `GET /api/form` and `GET /api/session/active` after every
   reconnect.
4. The contract test (tag `integration`) must assert message-list catch-up, and also that
   `/log` still does not replay, so an opencode upgrade that fixes it is noticed.

