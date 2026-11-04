# 0005: ACP is the universal agent adapter; native adapters are optional

Date: 2026-11-04. Status: accepted.

## Context

The spike (`docs/superpowers/specs/2026-11-04-opencode-v2-spike.md`) found that
agy's remote-control and remote-agent features are tied to Google's cloud with
no API, while `agy acp` works locally under the user's own login. `opencode acp`
also exists on the installed v2.2.5 and lists the same sessions as the opencode
service. ACP (Agent Client Protocol, JSON-RPC over stdio) is the one protocol
both of the user's agents speak. Other agents are expected to follow, some via
third-party adapters (not verified here).

## Decision

Build one ACP client adapter in Clara as the baseline for every agent. Sessions
are ACP sessions owned by Clara-supervised agent processes. Clara tracks them
with its own event log, resumes them with `session/load`, and lists them with
`session/list`.

Native adapters are optional enhancements, added per agent only if a concrete
need appears. The first candidate is opencode's HTTP service (forms, durable
per-session sequence, shared live state with TUIs).

## Consequences

- One implementation covers agy, opencode and future agents. Per-agent
  differences become small quirk handlers (e.g. agy quota text detection).
- ACP is a lowest common denominator: no structured questions (agy), no event
  sequence numbers or replay, no way to observe turns driven by another client.
  Attention is "turn ended", plus structured forms only where a native adapter
  exists.
- The terminal becomes a fallback attach, not a peer. For agy, do not attach a
  TUI to a conversation Clara has open. For opencode, `opencode attach` is
  possible because the service is shared (verify live sharing).
- Agent processes are children of Clara; restarts interrupt turns. Plan
  supervision and resume.
- Observed sessions (hand-started terminal agents) stay on the hooks-plus-tmux
  path and are optional.
- Facts to verify before relying on them: whether `opencode acp` shares live
  state with the background service, ACP cancellation and mid-turn prompts, and
  ACP support for the other agents the user may adopt.
