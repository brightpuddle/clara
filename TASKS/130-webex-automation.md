---
plan_recommended: false
---

# Webex Automation Intent

## Context

Webex is my primary work messaging platform. The signal-to-noise ratio is poor. Direct messages and
mentions require my attention; most group traffic does not. This intent manages visibility and
surfaces what actually requires me.

**Dependencies:**
- `060-clarabridge-extensions.md` - Webex MCP tools
- LLM MCP server (task 070)

## Workflow

### 1. Space/Group Triage

Read all joined Webex spaces and classify them.

Rules in `~/.config/clara/webex-rules.md` can:
- Hide bot/notification spaces
- Hide spaces inactive for 90+ days
- Mark large rooms as low-priority
- Flag important spaces

Unmatched spaces are classified with AI.

### 2. New Message Monitoring

Poll for new/unread messages.

For DMs and mentions:
- Always surface in priorities HUD
- Classify: `needs_reply`, `informational`, `actionable`
- Draft a suggested response

For group messages without a mention:
- Surface only if relevance is above threshold

### 3. Response Drafting

Generate suggested replies for messages that need a response. Surface in the TUI with quick actions.

### 4. Automatic Triage of Bot Messages

Read bot/notification spaces and only surface actionable alerts.

## Acceptance Criteria

- Bot/notification spaces are hidden on first run and remain hidden
- Direct messages and mentions appear in the priorities HUD within 10 minutes
- Draft replies are generated for messages needing a response
- No draft is sent without explicit approval
- Hidden spaces are logged and reversible via rule changes
- The rules file is watched for changes
