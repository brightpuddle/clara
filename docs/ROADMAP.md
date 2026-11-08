# Clara Roadmap

Master index of where Clara is going and in what order. Detailed task lists live in
`docs/superpowers/plans/`; this file is the map. Update it whenever direction changes
(and add an ADR for any reversal). Last revised: 2026-11-07.

## Principles

1. **Two personas.** *At the desk*: terminal and Neovim, keyboard only, zero friction.
   *Away*: triage, answer agent questions, nudge, dictate. Discord/Webex serve "away"
   today; a native iOS client replaces them later.
2. **One source of truth.** The Go agent core (`internal/agents`, event log in
   `clara.db`) owns sessions. Every client (terminal, Discord, Webex, later iOS) is a
   consumer of the same log. A session started from one surface is visible and
   drivable from all.
3. **Validation is releasing.** There is one user and no release. Each item ships to
   `main`, is used for real the same day, and bugs are fixed as found. Instead of
   long gates, every item has a one-line *daily-driver check* (see below).
4. **Workflow logic stays outside Go.** Automation lives in Bun/TS scripts calling MCP
   tools. Go is supervision, routing, tool hosting, and the agent core.
5. **Know where the logic lives.** See the code map below; keep it current.

## Code map (where is the logic?)

| Concern | Location |
|---|---|
| Agent core: ACP adapters, session runner, prompt queue, host log, hub | `internal/agents/` |
| `agent.*` MCP tools and bus events | `internal/builtin/agent/` |
| `clara agent` CLI (list/new/send/tail/cancel) | `cmd/clara/agent_sessions.go`, `agent_hub.go` |
| Discord/Webex router (client of the agent core) | `~/.config/clara/scripts/assistant/chat_router.ts`, `agent/` |
| Daily automation scripts (mail, digest, vault) | `~/.config/clara/scripts/{mail_triage,daily_digest,vault_*}` |
| Triggers, event bus, supervisor | `internal/trigger/`, `internal/supervisor/` |
| Tool registry and MCP servers | `internal/registry/`, `internal/server/` |
| Decisions / vocabulary / specs | `docs/adr/`, `docs/GLOSSARY.md`, `docs/superpowers/` |

Note: scripts under `~/.config/clara/scripts` are not in this repo (mail_triage was
partly committed under `scripts/`). Bringing the live scripts under version control is
tracked as item A5.

## Tracks and order

```
A Stabilise agent core ─► B Terminal cockpit ─► C Daily priorities ─► D Federation ─► E Mobile ─► F Retire Discord
        (now)               (next, biggest win)    (interleaved)         (Sophia)       (iOS)
                                                                         G Lucia (parked, research)
```

### Track A: Stabilise the agent core (now, small)
Stage 1 is merged and dogfooded on Eve. Finish the rough edges as they bite.
- [~] A1 Pin agy model: config `agents.adapters.agy.model` is set; pending daemon
      restart and a check that new agy sessions start on it. `agy acp` ignores
      `~/.gemini/antigravity-cli/settings.json`, so the Clara config is the source.
- [ ] A2 Router and daemon restart mid-turn: no lost or duplicated messages.
- [ ] A3 Webex smoke test for managed sessions.
- [ ] A4 Opt-in integration tests (tag `integration`) with cheap models.
- [ ] A5 Put live `~/.config/clara/scripts` under version control (or symlink from the repo).
- Daily-driver check: all new agent sessions for a week are managed, none via tmux.

### Track B: opencode peer-client adapter (next; see ADR 0008, supersedes 0007)
The desk client is stock `opencode attach` (Neovim via `ctrl-x e`). Clara does not
build a conversation TUI. Clara is a peer client of the opencode service.
- [x] B0 Close spike items (see spike doc, B0 findings): endpoint/auth discovery from Go, remote access auth
      (Tailscale-only vs `serve` auth), forms across service restart, log endpoint
      stability.
- [~] B1 opencode service client and mirror (new package `internal/agents/opencode`, not the
      ACP `adapter`): service lifecycle and discovery, `/api/event` SSE plus message-list
      catch-up (the `/log` endpoint does not replay in 2.2.6, see spike), prompts and form
      replies out, first-reply-wins on races, pinned version, fixtures.
      Done: client, mirror, store, daemon wiring (`agents.opencode_service`), `agent.attention.reply`,
      contract test (`-tags integration`, passes on 2.2.6). `agent.session.create`
      on the service. Left: `clara agent` reply command, cancel/close/model for mirrored
      sessions, first live run on Eve.
- [ ] B2 Mirror check: a prompt typed in the TUI appears in Discord/Webex and vice
      versa, with formatting preserved. Attention (needs you/done/failed) notifications.
- [ ] B3 Session picker: `clara agent list --json` plus a Neovim (Telescope/fzf) picker
      and a lualine status component. No dashboard TUI.
- [ ] B4 Freeze and then remove agy paths (ACP managed, hooks) once unused.
- Daily-driver check: for a week all sessions run on opencode and the remote surfaces
  stay in sync without tmux scraping.

### Track C: Daily priorities automation (interleaved with B)
Original goal: automatically handle incoming priorities (mail, Webex, reminders,
calendar).
- [x] C1 Mail triage: live execution with 116+ deterministic YAML rules + AI semantic
      fall-through; dynamic rule learning; Sophia satellite routing for Cisco Exchange;
      Apple Reminders creation with deep `message://` links and deduplication; running on schedule
      (`apple-mail-triage`, `apple-mail-unread-sweep`, `satellite-mail-triage`).
- [ ] C2 Webex triage: same pattern (rules + LLM fallback + audit).
- [ ] C3 Reminders and calendar ingestion into one prioritised daily view.
- [ ] C4 Daily digest unifies C1-C3 and open agent attentions (from B2).
- [ ] C5 Agent-assisted escalation: items that need thought become agent sessions.
- Daily-driver check: morning digest is read and acted on for a week.

### Track D: Federation (Stage 2 of the agent plan)
Sophia runs the same core; Eve's hub aggregates. Mostly built; needs live testing
over the tunnel and sleep/wake validation. Surfaces in the B3 picker as multi-host rows.

### Track E: Mobile (Stages 3 and 4 of the agent plan)
Priority may bubble up after B1/B2 (user, 2026-11-04); Discord covers mobile until then.
Client API (`/v1/snapshot|sync|stream|commands`), device registry, APNs, then the
iOS app (inbox first, session thread, dictation composer). Until then Discord is the
mobile client. Starts after B2, so the desk side is settled. Cheap first step: try `opencode web`
over Tailscale and Discord buttons for approvals before building a PWA or native app.

### Track F: Retire Discord/Webex for agent sessions (Stage 5)
Only after iOS is a reliable daily driver. Discord can stay for non-agent
notifications. The macOS native app is dropped in favour of stock opencode (ADR 0008).

### Track G: Lucia (parked)
`~/src/lucia/main`: type-safe constraints for AI-written code. Revisit once B and C
are in daily use. Until then, keep Clara conventions explicit (this file, AGENTS.md,
ADRs) so agents are guided correctly.

## Immediate order of work

1. B0 spike items, then B1 adapter, then B2 mirror check.
2. B3 picker, A1/A5, and C1 mail triage go-live (small, high daily value).
3. A2-A4 and D as dogfooding surfaces issues.
4. B4 agy removal, C2-C5, then E.

## Decisions pending

- Remote access to the opencode service: Tailscale-only binding vs `serve` auth.
- Mobile surface after Discord buttons: `opencode web` over Tailscale vs a Clara PWA.
