# Plan: Agent Core and Cockpit

Source of truth for task status. Process: `AGENTS.md` "Development Process".
Design: `docs/superpowers/specs/2026-11-04-cockpit-design.md`,
`.../2026-11-04-cockpit-event-schema-design.md`. Decisions: `docs/adr/`.
Legend: `[ ]` todo, `[~]` in progress, `[x]` done (short SHA).

## Stage 0: Design and spikes
- [x] Design spec, glossary, ADRs 0001 to 0006
- [x] opencode v2 API, ACP, cancel and busy-session spikes
- [x] Cleanup of stale `conductor/`; process moved into `AGENTS.md`

## Stage 1: Agent core, single host (merge to main)

Goal: managed agy and opencode sessions through one ACP adapter, with Discord and
Webex as the first clients, replacing tmux scraping for new sessions.

1.1 Foundations
- [x] a8fa2969 Move spike scripts to `docs/spikes/`; copy needed recordings to
      `internal/agents/testdata/` (scrub paths and secrets)
- [x] 6bcb6640 Wire schema: Go types for events, payloads, commands (hand-written first,
      generated later)
- [x] 149f6a9f `internal/agents/log`: host log in `clara.db` (append, read after
      `host_seq`, tests)
- [x] 394e4257 Projections rebuilt from events: sessions, attentions, prompt queue

1.2 ACP adapter
- [x] 2ede036c ACP JSON-RPC client over stdio (initialize, session/new, load, list,
      prompt, cancel; requests from agent answered; unknown methods rejected)
- [x] 2ede036c Normalise updates to `AgentEvent`; drop replayed history after `load`
- [x] 2ede036c Quirks table: agy (`set_model`, quota detection, early prompt response),
      opencode (config-option model selection, busy rejection)
- [x] 2ede036c Fixture-driven tests from recordings (cancel, busy, quota, tool calls)

1.3 Session runner
- [x] de1f3e83 Prompt queue (one in flight), turn tracking, "idle = latest prompt
      answered", cancel and interrupt
- [x] de1f3e83 State machine and attention (`turn_ended`, `blocked`, `failed`)
- [x] de1f3e83 Process supervision: per-session process, backoff, mark open turn
      `interrupted`, resume with `session/load`
- [x] de1f3e83 Concurrency and shutdown tests (context-bound goroutines, no leaks)

1.4 Clara integration
- [x] ba493cee `internal/builtin/agent`: MCP tools (`agent.session.create/list/get/close/
      resume/discover`, `agent.prompt.send/cancel`, `agent.turn.interrupt`,
      `agent.events.read`)
- [x] ba493cee CloudEvents `agent.*` published on the bus for each logged event
- [x] ba493cee `clara agent` CLI: list, new, send, tail, cancel
- [x] ba493cee Config section (`agents:`) and `clara types gen` coverage
- [~] Opt-in integration tests (tag `integration`) against real `agy acp` and
      `opencode acp` with cheap models

1.5 Discord and Webex cutover (TS router)
- [~] Channel to session mapping (topic `agent:<host>:<session>`); spawn creates
      managed sessions
- [~] Reactions and replies from `agent.turn.started/ended` and
      `agent.message`; queued-prompt feedback; `/kill` and interrupt
- [~] Catch-up on router restart via `agent.events.read`
- [~] Per-channel fallback flag to the old tmux path; observed sessions untouched
- [~] Disable the agy Discord hooks for managed sessions (no double posts)
      Code in repo, tests green; not yet installed on Eve (needs `make install-hooks`,
      `agent-relay` worker trigger from `examples/triggers/agent_relay.yaml`).
- [ ] Dogfood on Eve for a defined period; fix; then merge to `main`

Exit criteria: all new agent sessions on Eve run managed, with no tmux scraping
and no lost or duplicated messages across router and daemon restarts.

## Stage 2: Federation
- [ ] Same core on Sophia (`laptop.agent.*` via existing tunnel)
- [ ] Eve puller aggregating Sophia's log by `host_seq`; hub `seq` assignment and
      dedupe on `(host, host_seq)`
- [ ] `host.status` online/offline; command routing to the right host
- [ ] Sleep/wake test: no lost or duplicated events
- [ ] Router uses the hub view for both hosts

## Stage 3: Client API and push
- [ ] `/v1/snapshot`, `/v1/sync`, `/v1/stream`, `/v1/commands` with idempotency
- [ ] Device registration, tokens, revocation, read cursors
- [ ] Notification rules (one per attention) and APNs dispatcher; macOS via
      ClaraBridge
- [ ] `clara agent tail --hub` / replay tooling for UI-less verification

## Stage 4: iOS app (`swift/Cockpit/`)
- [ ] Swift package: types, sync client, local cache
- [ ] Inbox ("needs you" first), session thread with markdown, composer with
      dictation, queue view, interrupt
- [ ] Notification quick replies; TestFlight; device pairing

## Stage 5: macOS app and Discord retirement
- [ ] macOS target with multi-pane layout from shared views
- [ ] Retire Discord agent-session mirroring; migrate or drop
      `assistant_state.db` conversation tables (ADR 0001)

## Stage 6: Later
- [ ] Live Activities, hands-free voice, App Intents
- [ ] Native opencode adapter if forms or the durable log are worth it (ADR 0005)
- [ ] Observed-session adapter (hooks + tmux); terminal fallback view
