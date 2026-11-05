# 0007: Terminal-first desk client; native Mac app dropped

Date: 2026-11-04. Status: accepted. Supersedes parts of 0003 and the Stage 5 macOS
app in the plan.

## Context

ADR 0003 assumed the terminal was no longer a first-class UI and planned SwiftUI
clients for iPhone and Mac. Living with the managed agent core showed the desk
experience matters more than assumed: the user works entirely in the terminal
(WezTerm, tmux, Neovim with LSP and Copilot, fzf, git prompt) and composes prompts in
`$EDITOR`. A Mac app means leaving the keyboard flow. Raw `agy`/`opencode` TUIs lack
unified multiplexing, attention signals across ~8 concurrent agents, git context and
remote access. Driving them over ACP gave structure but removed the CLIs' chrome
(completion, session picker, model badge).

## Decision

1. The desk client is a terminal app in the `clara` binary, backed by the same agent
   core and event log as every other client:
   - `clara agent attach <session>`: interactive session client. `Ctrl-G` or `/edit`
     opens `$EDITOR` for the prompt. Shows model, git state, session state, queue.
   - `clara cockpit`: lazygit-style dashboard of all sessions across hosts, tightly
     integrated with `attach` (open a session full screen, return to the list).
2. No native macOS app. The iOS app (Track E) remains the away client.
3. No Neovim plugin for now. Prompt editing via `$EDITOR` covers the need; revisit an
   Oil-style agent buffer later.
4. tmux is a fallback and observed-session path only, not the multiplexer.

## Consequences

- ADR 0003 still holds for iOS; the macOS target and the "multi-pane macOS layout"
  are removed from scope.
- The terminal client becomes the first real consumer of the client-facing data
  model, which de-risks the iOS API.
- Slash-command and completion parity with agy/opencode depends on what ACP
  advertises (available commands, models, modes); gaps are documented, not worked
  around with scraping.
- A new Bubble Tea app is added despite the earlier TUI removal; scope stays narrow
  (sessions only, no chat REPL for tools).
