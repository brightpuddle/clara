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

## Not yet verified (do next)

- Event types and payloads on `/api/event`. A 10 second sample while idle
  returned nothing. Re-sample during an active turn and while a form is pending
  and record the real event names. The OpenAPI event union did not parse with
  a quick script; read `V2EventEncoded` by hand or use `@opencode/client`.
- That the agent's question tool creates a Form and that `reply` unblocks it.
  Trigger one deliberately and observe `/api/form`.
- Authentication when listening beyond localhost, and how `serve` differs from
  the background service (stable port, auth, `--cors`).
- Whether a prompt sent via API shows up live in a running TUI (it should, as
  both are clients of one service).

## agy (not started)

Still to do: confirm hook payloads for `agy` 1.5.0 on a long-lived session,
whether a question to the user produces any hook or transcript signal, and
whether anything similar to opencode's service API exists (`agy --help`).
