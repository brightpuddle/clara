# Clara Roadmap

Master index of where Clara is going and in what order. Detailed task lists live in
`docs/superpowers/plans/`; this file is the map. Update it whenever direction changes
(and add an ADR for any reversal). Last revised: 2026-11-04.

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
- [ ] A1 Pin agy model: config `agents.adapters.agy.model` (set it in
      `~/.config/clara/config.yaml`; verify `agy acp` honours it via `set_model`), and
      document that `agy acp` ignores `~/.gemini/antigravity-cli/settings.json`.
- [ ] A2 Router and daemon restart mid-turn: no lost or duplicated messages.
- [ ] A3 Webex smoke test for managed sessions.
- [ ] A4 Opt-in integration tests (tag `integration`) with cheap models.
- [ ] A5 Put live `~/.config/clara/scripts` under version control (or symlink from the repo).
- Daily-driver check: all new agent sessions for a week are managed, none via tmux.

### Track B: Terminal cockpit (next; see ADR 0007)
One Go app inside the `clara` binary, lazygit-style. Plan: `plans/2026-11-04-terminal-cockpit.md`.
- B1 `clara agent attach <session>`: interactive client. `Ctrl-G` or `/edit` opens
  `$EDITOR` (Neovim) for the prompt; header shows session, agent, model, git branch
  and dirty state, state (idle/busy/needs you), queue depth; live event stream; slash
  command and model completion from the ACP-advertised commands; resume by session id.
- B2 Attention signals: notification (and optional tmux/WezTerm status text) when any
  session goes `busy -> needs_you/done/failed`. Fixes the "eight agents silently
  waiting" problem.
- B3 `clara cockpit`: full-screen dashboard of all sessions on all hosts (state, dir,
  branch, model, last message, age), fzf-style filter, `Enter` opens the B1 client
  full screen and `q`/`Esc` returns. Same binary, shared components.
- B4 Polish: shell prompt/status helpers, `new` flow (host, dir, agent, model),
  git actions (status/diff/commit helper) if wanted.
- Daily-driver check: for a week, no tmux window hopping to see agent state.

### Track C: Daily priorities automation (interleaved with B)
Original goal: automatically handle incoming priorities (mail, Webex, reminders,
calendar). Mail triage engine and daily digest exist as scripts; their current
validation state should be confirmed before building more. Plan:
`plans/2026-11-04-daily-priorities.md` (to be written when B1 lands).
- C1 Mail triage: shadow mode review, then `--execute` for high-confidence rules;
  trigger on a schedule; summary to `#notify`.
- C2 Webex triage: same pattern (rules + LLM fallback + audit).
- C3 Reminders and calendar ingestion into one prioritised daily view.
- C4 Daily digest unifies C1-C3 and open agent attentions (from B2).
- C5 Agent-assisted escalation: items that need thought become agent sessions.
- Daily-driver check: morning digest is read and acted on for a week.

### Track D: Federation (Stage 2 of the agent plan)
Sophia runs the same core; Eve's hub aggregates. Mostly built; needs live testing
over the tunnel and sleep/wake validation. Surfaces in B3 as multi-host rows.

### Track E: Mobile (Stages 3 and 4 of the agent plan)
Client API (`/v1/snapshot|sync|stream|commands`), device registry, APNs, then the
iOS app (inbox first, session thread, dictation composer). Until then Discord is the
mobile client. Starts after B3, so the desk side is settled and the API is proven by
the terminal client first (the terminal client should consume the same hub API where
practical, so E is "add iOS" rather than "design the protocol").

### Track F: Retire Discord/Webex for agent sessions (Stage 5)
Only after iOS is a reliable daily driver. Discord can stay for non-agent
notifications. The macOS native app is dropped in favour of Track B (ADR 0007).

### Track G: Lucia (parked)
`~/src/lucia/main`: type-safe constraints for AI-written code. Revisit once B and C
are in daily use. Until then, keep Clara conventions explicit (this file, AGENTS.md,
ADRs) so agents are guided correctly.

## Immediate order of work

1. A1 (config change, minutes), A5.
2. B1 attach client (first code), then B2.
3. C1 mail triage shadow review and go-live (small, high daily value).
4. B3 cockpit dashboard.
5. A2-A4 and D as dogfooding surfaces issues.
6. C2-C5, then E.

## Decisions pending

- TUI stack for B (proposed: Go with Bubble Tea/Bubbles; AGENTS.md notes the old
  TUI was deleted, this is a new, narrower app). See the B plan.
- Whether `attach` talks to the daemon over IPC/MCP now or directly to the hub API
  (proposed: via existing MCP tools first, hub API when Track E lands).
