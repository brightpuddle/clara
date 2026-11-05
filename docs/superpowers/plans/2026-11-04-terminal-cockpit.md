# Plan: Terminal Cockpit (Track B)

Source of truth for task status. Process: `AGENTS.md` "Development Process".
Roadmap: `docs/ROADMAP.md`. Decision: `docs/adr/0007-terminal-first-desk-client.md`.
Legend: `[ ]` todo, `[~]` in progress, `[x]` done (short SHA).

## B0 Prerequisites (research, small)
- [ ] Inventory what ACP advertises for agy and opencode: available commands, models,
      modes, session list/load. Decide the completion sources for slash commands.
- [ ] Confirm what `agent.*` tools/events give a client: state, model, queue depth,
      cwd, last message. List gaps (e.g. git info computed client-side).
- [ ] Choose TUI stack (proposed: Bubble Tea + Bubbles + Lip Gloss) and package
      layout: `internal/cockpit/` (UI components, no daemon logic), `cmd/clara/`
      wiring only.

## B1 `clara agent attach <session>`
- [ ] Session resolver: id, unique prefix, title, or fzf-style picker when omitted;
      `--resume <agy|opencode session id>` adopts an existing agent session via
      `agent.session.discover/resume`.
- [ ] Header: session id/title, agent, host, dir, git branch + dirty/ahead-behind
      (client-side `git`), model, state, queue depth.
- [ ] Transcript view driven by `agent.events.read` + bus stream (tail with cursor),
      markdown rendering, tool calls collapsed.
- [ ] Prompt input: multiline, history; `Ctrl-G` and `/edit` open `$EDITOR` on a temp
      markdown file, send on save-and-quit if non-empty; abort on empty.
- [ ] Slash commands and `.model`-style commands: completion from B0 findings; `/model`,
      `/cancel`, `/interrupt`, `/queue`, `/detach`.
- [ ] Busy-turn behaviour: enqueue, show queue, cancel queued prompt, interrupt.
- [ ] Tests: model/update logic as pure functions; fixture-driven event rendering; no
      goroutine leaks (context-bound).
- Daily-driver check: one real working day driving agents from attach only.

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
