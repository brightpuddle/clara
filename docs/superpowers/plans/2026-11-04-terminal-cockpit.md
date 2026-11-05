# Plan: Terminal Cockpit (Track B)

Source of truth for task status. Process: `AGENTS.md` "Development Process".
Roadmap: `docs/ROADMAP.md`. Decision: `docs/adr/0007-terminal-first-desk-client.md`.
Legend: `[ ]` todo, `[~]` in progress, `[x]` done (short SHA).

## B0 Prerequisites (research, small)
- [x] 6560f385 Inventory what ACP advertises for agy and opencode: available commands, models,
      modes, session list/load. Decide the completion sources for slash commands.
- [x] 6560f385 Confirm what `agent.*` tools/events give a client: state, model, queue depth,
      cwd, last message. List gaps (e.g. git info computed client-side).
- [x] 6560f385 Choose TUI stack (Bubble Tea + Bubbles + Lip Gloss) and package
      layout: `internal/cockpit/` (UI components, no daemon logic), `cmd/clara/`
      wiring only. Dependencies added to go.mod.

## B1 `clara agent attach <session>`
- [x] 0607a13f Session resolver: id, unique prefix, suffix, title, or newest active session when omitted.
- [x] 0607a13f Header: session id/title, agent, host, dir, git branch + dirty/ahead-behind (client-side `git`), model, state, queue depth.
- [x] 0607a13f Transcript view driven by `agent.events.read` live polling, user/assistant/tool messages rendered.
- [x] 0607a13f Prompt input: multiline buffer, `Ctrl-G` and `/edit` open `$EDITOR` (Neovim) on a temp markdown file, send on save-and-quit.
- [x] 0607a13f Slash commands: `/model [name]`, `/cancel`, `/interrupt`, `/detach`, `/edit`.
- [x] 0607a13f Busy-turn behaviour: enqueue, spinner indicator, live event stream during turn.
- [x] 0607a13f Tests: session resolver, git status parser, slash command parser, event renderer.
- [ ] Daily-driver check: one real working day driving agents from attach only.

## B2 Attention signals
- [ ] Subscribe to `agent.*` events; on `busy -> needs_you|done|failed|blocked` emit a
      desktop notification (ClaraBridge) with session name and last line.
- [ ] `clara agent status --short` for tmux/WezTerm status bars (counts per state).
- [ ] Config: which states notify, quiet hours, suppress for the session currently
      attached in a terminal.

## B3 `clara cockpit` dashboard
- [ ] List all sessions across hosts (hub view): state, host, dir, branch, model,
      last message, age; sorted needs-you first.
- [ ] Keys: `j/k`, `/` filter, `Enter` open attach full screen, `Esc/q` back, `n` new
      session flow, `x` close, `i` interrupt, `m` model.
- [ ] Shared model/components with B1; attach is a screen of the same app.
- Daily-driver check: no tmux window hopping to learn agent state for a week.

## B4 Polish
- [ ] Zsh helper/completion for session names; optional fzf integration.
- [ ] Git helpers from the header (status/diff view, commit message via configured
      tool) only if wanted after living with B1-B3.
- [ ] Update `AGENTS.md` CLI surface and `docs/GLOSSARY.md`.
