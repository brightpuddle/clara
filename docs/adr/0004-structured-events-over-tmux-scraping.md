# 0004: Structured agent events are the source of truth; tmux is runtime

Date: 2026-11-04. Status: accepted. opencode confirmed viable by spike; see
`docs/superpowers/specs/2026-11-04-opencode-v2-spike.md`. agy confirmed via ACP.

## Context

opencode output is currently derived from `tmux capture-pane` with spinner
regexes and TUI-glyph stripping, which is fragile. agy already uses hooks and
transcripts. The user's agents run fully permissive and only interrupt to ask
questions.

## Decision

Adapters produce structured Events from each agent's own protocol:

- **opencode:** HTTP client of the v2 background service. Sessions already
  live there; the TUI is just another client. Uses the global event stream
  plus the durable per-session log for lossless catch-up.
- **agy:** driven over ACP (`agy acp`, JSON-RPC over stdio) as a
  Clara-supervised process. Hooks and tmux are only for sessions started by
  hand in a terminal ("observed" sessions), which are optional.

tmux remains a runtime for hand-started agents and a manual-access fallback.
It is never parsed for content except as a documented last resort.

## Consequences

- Removes the largest source of fragility in the current chat path.
- opencode sessions need no migration; agy sessions move from tmux TUI to
  Clara-managed ACP processes.
- Attention differs by agent: opencode forms are structured questions; agy has
  only end of turn. The schema supports both.
- agy has no event sequence numbers or replay, and reports quota exhaustion as
  ordinary text. The adapter must handle both.
- Details and evidence: `docs/superpowers/specs/2026-11-04-opencode-v2-spike.md`.
