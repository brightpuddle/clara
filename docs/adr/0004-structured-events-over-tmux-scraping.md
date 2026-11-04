# 0004: Structured agent events are the source of truth; tmux is runtime

Date: 2026-11-04. Status: accepted. opencode confirmed viable by spike; see
`docs/superpowers/specs/2026-11-04-opencode-v2-spike.md`. agy still open.

## Context

opencode output is currently derived from `tmux capture-pane` with spinner
regexes and TUI-glyph stripping, which is fragile. agy already uses hooks and
transcripts. The user's agents run fully permissive and only interrupt to ask
questions.

## Decision

Adapters produce structured Events. opencode v2 is already client-server, so
its adapter is an HTTP client of the background service (sessions already live
there; the TUI is just another client). agy keeps hooks plus transcripts. tmux remains the
runtime for agy and a manual-access fallback; it is never parsed for content
except as a documented last resort.

## Consequences

- Removes the largest source of fragility in the current chat path.
- Existing TUI sessions in tmux need switching to server-managed sessions.
- agy question detection is an open risk (see design spec section 4).
