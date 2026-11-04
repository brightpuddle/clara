# 0006: Build and merge the agent core before any client

Date: 2026-11-04. Status: accepted.

## Context

Clara already uses agents through Discord and Webex, with tmux scraping for
opencode and hooks for agy. The new Cockpit clients are a large effort, but the
agent management underneath them is useful immediately and is the riskiest part
(protocol quirks, supervision, queueing).

## Decision

Deliver in stages. Stage 1 is the agent core as a standalone Clara capability:
`internal/agents` (ACP adapters, session runner, prompt queue, host log), a
builtin `agent` MCP namespace, `agent.*` CloudEvents on the event bus, and a
`clara agent` CLI. The existing TypeScript chat router is migrated to use it for
Discord and Webex, with tmux kept as a per-channel fallback. Stage 1 is merged
to `main` and run in daily use before federation, the client API, or any app.

The event envelope and schema are designed for the hub from the start so later
stages add no breaking changes.

## Consequences

- Immediate benefit and real-world validation of the riskiest code.
- Discord/Webex become the first clients of the log, proving it is client
  agnostic.
- Stage 1 must not depend on later stages; hub and client API code is not
  merged with it.
- Spike scripts and recordings must move out of the repo root (to
  `docs/spikes/` and `testdata/`) before merging to main.
