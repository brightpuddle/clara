# Clara Roadmap

Master index of where Clara is going and in what order. Detailed task lists live in
`docs/superpowers/plans/`; this file is the map. Update it whenever direction changes
(and add an ADR for any reversal). Last revised: 2026-11-24.

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
| --- | --- |
| Agent core: ACP adapters, session runner, prompt queue, host log, hub | `internal/agents/` |
| `agent.*` MCP tools and bus events | `internal/builtin/agent/` |
| `clara agent` CLI (list/new/send/tail/cancel) | `cmd/clara/agent_sessions.go`, `agent_hub.go` |
| Discord/Webex router (client of the agent core) | `~/.config/clara/scripts/assistant/chat_router.ts`, `agent/` |
| Daily automation scripts (mail, digest, vault) | `~/.config/clara/scripts/` (own git repo, `brightpuddle/clara-workflows`; ADR 0012) |
| Per-host config and enabled triggers | `~/.config/clara/{config.yaml,hosts/,conf.d/}`; `hosts/apply.sh` |
| Secrets (`${keychain:op-*}`), `clara secrets check` | `internal/config/secrets.go`, `cmd/clara/secrets.go` |
| Triggers, event bus, supervisor | `internal/trigger/`, `internal/supervisor/` |
| Tool registry and MCP servers | `internal/registry/`, `internal/server/` |
| Decisions / vocabulary / specs | `docs/adr/`, `docs/GLOSSARY.md`, `docs/superpowers/` |

Note: live workflows are not in this repo. They live in the separate workflows repo at
`~/.config/clara` (ADR 0012, done as A5/Track S); the clara repo holds no personal workflows.

## Tracks and order

```
A Stabilise agent core ─► B Terminal cockpit ─► C Daily priorities ─► D Federation ─► E Mobile ─► F Retire Discord
        (now)               (next, biggest win)    (interleaved)         (Sophia)       (iOS)
                                                                         G Lucia (parked, research)

S Secrets and workflows repo (done) ─► I Run visibility ─► H Unified memory (spike)
                                        (feeds C4 digest)    (uses I's receipts as a source)
```

Tracks S, I and H are one workstream ("make Clara legible and context-aware"); they are
independent of B-F and can interleave with C. Order inside it: S, then I, then H.

### Track A: Stabilise the agent core (now, small)

Stage 1 is merged and dogfooded on Eve. Finish the rough edges as they bite.

- [x] A1 Dropped with B4 (agy support removed).
- [ ] A2 Router and daemon restart mid-turn: no lost or duplicated messages.
- [ ] A3 Webex smoke test for managed sessions.
- [ ] A4 Opt-in integration tests (tag `integration`) with cheap models.
- [x] A5 Live `~/.config/clara` workflows are in their own git repo (ADR 0012); secrets resolve from the keychain.
- Daily-driver check: all new agent sessions for a week are managed, none via tmux.

### Track B: opencode peer-client adapter (next; see ADR 0008, supersedes 0007)

The desk client is stock `opencode attach` (Neovim via `ctrl-x e`). Clara does not
build a conversation TUI. Clara is a peer client of the opencode service.

- [x] B0 Close spike items (see spike doc, B0 findings): endpoint/auth discovery from Go, remote access auth
      (Tailscale-only vs `serve` auth), forms across service restart, log endpoint
      stability.
- [x] B1 opencode service client and mirror (new package `internal/agents/opencode`, not the
      ACP `adapter`): discovery/lifecycle, `/api/event` SSE plus message-list catch-up,
      prompts, form replies (first reply wins), interrupt, close (stops mirroring, leaves
      the opencode session intact), model switch, `agent.session.create`, pinned 2.2.6,
      fixtures, contract test. CLI: `clara agent {attention,reply,close,model,list --json}`.
- [x] B2 Mirror check: TUI prompts appear in the session channel (`🖥️ **TUI:**`), chat prompts
      keep their origin through the service (reactions work), questions are posted with
      numbered options and answered by replying in the channel (`attentionAnswer` in
      `relay_core.ts`, router calls `agent.attention.reply`); resolution elsewhere is
      announced. Live on Eve (`agents.opencode_service`, `max_session_age: 2h`): create,
      form, reply, prompt, model switch and close verified end to end.
- [x] B3 Session picker: `clara agent list --json`, `editors/nvim/clara_agents.lua`
      (picker `<leader>aa`, lualine component, copy to `~/.config/nvim/lua`).
- [x] B4 Freeze and then remove agy paths (ACP managed, hooks) once unused.
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

### Track E: Mobile (revised 2026-11-09; ADR 0009, ADR 0010)

The opencode web UI is the mobile client. Verified on Eve (service plus `opencode pair`):
it organises sessions and projects the way we want. Clara does not build a PWA, a native
app or the `/v1` client API for now. Wait for the official opencode iOS app rather than
building one.

- [x] E1 Install Tailscale on Eve (key expiry off, no sleep, unattended recovery) and
      expose the service with `tailscale serve`. Pair the phone and add to Home Screen.
- [x] E2 Notifications: Discord/Webex attention pings carry deep links into the web UI.
- [x] E3 Sophia sessions: its service is only reachable over the SSH tunnel (Tailscale
      conflicts with Cisco VPN). Proxy it through Eve or accept a separate entry point.
- Known gaps: iOS web push and background reconnect are weak; Discord stays the
  notification channel.
- Daily-driver check: a week of triaging and answering agent sessions from the phone.

### Track F: Retire Discord/Webex for agent sessions (Stage 5)

Only after the opencode web or iOS client is a reliable daily driver. Discord can stay for non-agent
notifications. The macOS native app is dropped in favour of stock opencode (ADR 0008).

### Track S: Secrets and workflows repo (done 2026-11-24; ADR 0012)

Prerequisite for I: scripts must be editable under version control and free of secrets.

- [x] S1 `${keychain:service}` in config (resolved after YAML parse, never re-expanded;
      missing item fails startup); `config.Save` keeps references; file mode 0600 (`9805db95`).
- [x] S2 `clara secrets check` (also scans `conf.d/`) (`2990b3f5`).
- [x] S3 1Password vault `Clara` is source of truth, synced to keychain as `op-<title>`;
      `config.yaml` has no plaintext secrets (webhook secret included).
- [x] S4 `~/.config/clara` is its own git repo (allowlist `.gitignore`, README), pushed to
      `brightpuddle/clara-workflows`; Sophia cloned and tracking `origin`.
- [x] S5 Per-host config: shared `config.yaml` plus `hosts/<host>.yaml` via `conf.d/host.yaml`;
      triggers per host via `hosts/<host>.triggers` and `hosts/apply.sh`. Eve runs the
      triggers and drives Sophia via `laptop.*`; Sophia runs none.
- [~] S6 Rebuild and install the current clara on both hosts (installed binaries predate
      `secrets check` scanning `conf.d/` and S9), then restart both daemons on the new layout
      (Eve's daemon dates from Nov 12, Sophia's from Nov 17) and confirm.
- [x] S7 Reboot Eve untouched: tested. A fresh login makes the keychain ask for explicit
      approval of the secrets; accepted (one user, host rarely reloads). See ADR 0012.
- [ ] S8 Finish rotating tokens exposed in the old plaintext config; delete stale copies.
- [x] S9 `clara mcp add/remove` edit only the owning YAML file (new entries go to
      `conf.d/host.yaml`); the merged config is never written back, so references, comments and
      the base/host split survive. `config.Save` removed.
- Daily-driver check: `clara secrets check` clean on both hosts after a reboot.

### Track I: Run visibility (plan: `docs/superpowers/plans/2026-11-20-run-visibility.md`)

After-the-fact only: what did Clara do, and is it working as intended? Linked run records,
per-run receipts (`run.report`), declared `expect:` blocks per trigger, `clara health`,
`clara report`, a daily vault note and digest, later a cockpit Runs tab.

- [x] I1 Linked records (`event_id`, `parent_run_id`, `session_id`, `receipt`) and `run.report`;
      adopt in `mail_triage` first.
- [x] I2 `expect:` in trigger schema; baselines; `clara health` (healthy/degraded/failing/silent).
- [~] I3 `clara report`, daily vault note, Webex/Discord digest with alerts for failing/silent.
      Folds into C4 (daily digest).
- [ ] I4 Cockpit Runs tab (only after I1-I3 are used).
- Daily-driver check: for a week the digest alone says whether anything is wrong.

### Track H: Unified memory (spike; plan: `docs/superpowers/plans/2026-11-20-memory-spike.md`)

One small MCP surface (`memory.search`, `memory.get`, maybe `memory.remember`) over zk, docs,
and notable runs. Gated: it must beat today's tools on a real question set, or it stops.

- [ ] H0 Eval set of real questions plus baseline with today's tools; check opencode overlap.
- [ ] H1 Keyword (FTS5) memory over zk and docs.
- [ ] H2 Vectors (local embeddings only for work content, ADR first) and run history (uses
      Track I receipts).
- [ ] H3 Write-back and federation, only if H1/H2 pay off.
- Daily-driver check: used unprompted for a week.

### Track G: Lucia (parked)

`~/src/lucia/main`: type-safe constraints for AI-written code. Revisit once B and C
are in daily use. Until then, keep Clara conventions explicit (this file, AGENTS.md,
ADRs) so agents are guided correctly.

## Immediate order of work

1. B0 spike items, then B1 adapter, then B2 mirror check. (Done.)
2. B3 picker, A1/A5, and C1 mail triage go-live. (Done; Track S done alongside.)
3. S6-S8 (finish the secrets cut-over), then I1-I2 (receipts and health), starting with
   mail triage.
4. H0 (eval set) can run any time; H1+ after I1 so run history has receipts.
5. A2-A4 and D as dogfooding surfaces issues; C2-C5 (I3 folds into C4), then E.

## Decisions pending

- Workflows repo and secrets: decided (ADR 0012). Open: whether the host-config split needs
  an ADR amendment once S9 lands.
- Embeddings for confidential (Cisco) content must be local; needs an ADR before H2.

- Remote access to the opencode service: decided, Tailscale-only (ADR 0009). Sophia keeps
  the SSH tunnel because Tailscale does not work with Cisco VPN.
- Mobile surface: decided, opencode web UI (ADR 0010). Revisit when the official opencode
  iOS app ships.
