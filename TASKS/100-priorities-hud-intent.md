---
plan_recommended: false
---

# Priorities HUD Intent

## Context

When I sit in front of my computer, I want to immediately know what needs my attention. This intent
continuously aggregates items from all data sources, prioritizes them, pre-triages them with AI
where possible, and surfaces the top items to my TUI as an always-current attention queue.

This is the central nervous system of Clara's personal assistant function. It transforms Clara
from a passive tool into an active assistant that reduces cognitive overhead.

**Dependencies:**
- `060-clarabridge-extensions.md`
- GitHub MCP server
- Taskwarrior MCP server
- ZK MCP server
- LLM MCP server (task 070)

## Data Sources

The intent aggregates from:
- Apple Reminders
- Apple Calendar
- Apple Mail
- GitHub
- Taskwarrior
- ZK Vault
- Webex

## Priority Scoring

Items are scored on:
1. **Time sensitivity**
2. **Source weight**
3. **Explicit priority**
4. **Staleness**

The scoring logic is defined in `~/.config/clara/priorities.md` and watched for changes.

## AI Pre-triage

For each incoming attention item, the intent uses the LLM MCP server to pre-generate:
- A one-sentence summary
- A suggested response or action
- Relevant context links

## Output to TUI

Prioritized items are pushed to the TUI via `notify_send_interactive`.

## Intent Design

- **Schedule**: polls every 5 minutes by default
- **Event-driven** refresh option
- **Deduplication** via SQLite
- **Snooze** support

## Acceptance Criteria

- Reminders due today appear in the TUI within 5 minutes
- Calendar events in the next 24 hours appear with countdown
- Unread emails appear with AI-generated summary and draft response link
- GitHub items assigned to me or mentioning me appear with context
- Priority ordering reflects the configured heuristics
- Dismissed items do not reappear
- Snooze works correctly
- Priorities config markdown changes take effect on the next run
