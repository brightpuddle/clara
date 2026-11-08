# 0008: opencode is the primary agent; Clara is a peer client of its service

Date: 2026-11-07. Status: accepted. Supersedes 0007 and the "ACP baseline" part of 0005.

## Context

Building a Clara-owned terminal client (ADR 0007, Track B) means re-implementing what
mature agentic CLIs already do: wrapping, editor integration, session resume, approvals.
That is a long tail of QoL work we do not want to own.

The goals are:

1. At the desk, use a mature agentic CLI.
2. Local and remote surfaces (terminal, Discord, Webex, later mobile) always show the
   same session content, live, in both directions.
3. Personal work and corporate Copilot work both flow through the same agent.

Findings:

- opencode v2 is client/server. The TUI is a client of a shared background service
  (`opencode attach`, `--server`). The service exposes durable per-session events
  (with sequence numbers and replay), forms (questions and approvals), and prompt and
  reply endpoints. A TUI and Clara can therefore be peers on one live session.
  This meets goal 2 with no frontend code. See the opencode v2 spike.
- opencode reaches the corporate Copilot service (via copilot-gateway) and the
  personal OpenCode Go subscription (DeepSeek V4 Pro), so goal 3 holds with one agent.
- agy cannot meet goal 2: its TUI has no attach or event API, `agy acp` is a child
  process of one client, and its remote-control features are tied to Google's cloud.
  Every agy path was a special case in the architecture. The Gemini Pro subscription
  stays for six more months but is no longer a Clara dependency.

## Decision

- opencode is the primary and only first-class agent.
- Clara is a peer client of the opencode service. It subscribes to the session event
  stream, mirrors events into the Clara log and event bus, and sends prompts and
  form replies through the service API. It supervises the service process.
- The desk client is the stock `opencode attach` TUI (with Neovim via `ctrl-x e`).
  Do not build a Clara TUI for conversations (Track B B1 is dropped).
- Remote surfaces (Discord, Webex, later PWA or native) are other peer clients.
- Keep the `AgentAdapter` interface and normalized event schema so another agent can be
  added later, but write no second adapter until a concrete need exists.
- agy support (ACP managed sessions, observed hooks) is frozen: no new work. Remove it
  once nothing depends on it.

## Consequences

- Terminal and remote stay in sync by construction. No tmux scraping and no
  turn-granular hook approximations.
- Clara is coupled to opencode's service API, which is still moving. Mitigate by pinning
  the version, recording fixtures in `testdata/`, and keeping all API use inside one
  adapter package.
- Permission and form replies can race between the TUI and a remote client. First reply
  wins; the adapter must tolerate "already resolved".
- A session-list view is not a TUI to build: expose `clara agent list --json` and use an
  fzf or Telescope picker. `clara top` and the cockpit dashboard (B3) are dropped or
  reduced to that.
- Remaining open items from the spike: endpoint and auth discovery from Go, remote
  access auth (Tailscale-only binding vs `serve` auth), stability of the log endpoint
  across releases, and whether unanswered forms survive a service restart.
