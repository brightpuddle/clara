# 0004: Structured agent events are the source of truth; tmux is runtime

Date: 2026-11-04. Status: accepted (opencode details pending spike).

## Context

opencode output is currently derived from `tmux capture-pane` with spinner
regexes and TUI-glyph stripping, which is fragile. agy already uses hooks and
transcripts. The user's agents run fully permissive and only interrupt to ask
questions.

## Decision

Adapters produce structured Events. opencode moves to its server API
(`opencode serve`, with `opencode attach` for desktop TUI access), to be
verified in a spike. agy keeps hooks plus transcripts. tmux remains the
runtime for agy and a manual-access fallback; it is never parsed for content
except as a documented last resort.

## Consequences

- Removes the largest source of fragility in the current chat path.
- Existing TUI sessions in tmux need switching to server-managed sessions.
- agy question detection is an open risk (see design spec section 4).
