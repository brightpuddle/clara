# Plan: Cockpit

Follow `conductor/workflow.md`: tests first, mark `[~]` when starting, commit per
task. Phase 0 is done.

## Phase 0: Design and spikes (done)
- [x] Design spec, glossary, ADRs 0001 to 0005
- [x] opencode v2 API, ACP, cancel and busy-session spikes

## Phase 1: Protocol and storage (Go, no agents)
- [ ] Define wire schema file (events, commands, snapshot) and generate Go types
- [ ] `internal/cockpit/log`: host log (append, read after `host_seq`, tests)
- [ ] Session, attention, prompt-queue projections rebuilt from events (tests)
- [ ] Hub ingest: assign `seq`, dedupe on `(host, host_seq)` (tests)
- [ ] `/v1/sync`, `/v1/snapshot`, `/v1/stream`, `/v1/commands` with idempotency
- [ ] Device registration, tokens, revocation
- [ ] `clara cockpit tail|replay` CLI for UI-less verification

## Phase 2: ACP adapter and session runner
- [ ] ACP JSON-RPC client over stdio (initialize, new, load, list, prompt, cancel)
- [ ] Normalise ACP updates to `AgentEvent`; drop replayed history after `load`
- [ ] `SessionRunner`: prompt queue, turn tracking, state, attention
- [ ] Quirks: agy (`set_model`, quota detection, early prompt response),
      opencode (config-option model selection, busy rejection)
- [ ] Process supervision with backoff; interrupt and resume policy
- [ ] Fixture-driven tests from `spike/out/*.jsonl` recordings
- [ ] Integration test against real `agy acp` / `opencode acp` (tagged, opt-in)

## Phase 3: Federation
- [ ] Host-mode endpoint serving the local log by `host_seq`
- [ ] Hub puller over the Sophia tunnel with reconnect and catch-up
- [ ] `host.status` online/offline; commands routed to the right host
- [ ] Sleep/wake test: no lost or duplicated events

## Phase 4: Notifications
- [ ] Attention and status rules; single notification per attention
- [ ] APNs dispatcher (key on Eve), delivery and token cleanup
- [ ] macOS notifications through ClaraBridge

## Phase 5: iOS app (SwiftUI, `swift/Cockpit/`)
- [ ] Swift package with generated types, sync client, local cache
- [ ] Inbox ("needs you" first), session thread with markdown, composer
- [ ] Dictation in composer; queue view; interrupt button
- [ ] Notification actions (quick replies)
- [ ] TestFlight build and device pairing flow

## Phase 6: macOS app and transition
- [ ] macOS target, multi-pane layout from shared views
- [ ] Discord as a client adapter over the log; retire agent-session mirroring
- [ ] Migrate or retire `assistant_state.db` tables (ADR 0001)

## Phase 7: Later
- [ ] Live Activities, hands-free voice, App Intents
- [ ] Native opencode adapter if forms or durable-log value is needed (ADR 0005)
- [ ] Observed-session adapter (hooks + tmux)
- [ ] Terminal fallback view
