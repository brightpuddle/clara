---
plan_recommended: false
---

# Email Automation Intent

## Context

My email inbox is a source of stress and cognitive load. Messages requiring action are buried
among newsletters, automated notifications, and low-priority threads. This intent progressively
moves me toward inbox zero by automatically triaging, archiving, and pre-generating responses.

**Dependencies:**
- `060-clarabridge-extensions.md` - Apple Mail MCP tools
- LLM MCP server (task 070)

## Behavior Philosophy

- **Never delete permanently**
- **Never send autonomously**
- **Always log**
- **Learn from corrections**

## Workflow

### 1. Inbox Scan

Read all unread messages in inbox via `mail.list_inbox`. If not already in `email_queue`, classify it.

### 2. Classification

**Automated rules (fast path):**
Rules in `~/.config/clara/email-rules.md` are compiled into a structured ruleset.

Examples:
- Archive newsletters and promotional emails
- Route GitHub emails to github
- Flag emails from my manager or skip-level as high priority

**AI classification (slow path):**
For unmatched messages, use `llm.generate` to classify as:
- `needs_reply`
- `needs_review`
- `informational`
- `noise`

Generate draft replies where needed.

### 3. Actions

- `noise` -> Archive
- `informational` -> Archive or label
- `needs_review` -> Flag, surface in HUD
- `needs_reply` -> Create draft, surface in HUD
- Rule-matched -> Per rule

### 4. Surface in TUI

High-priority emails surface in the priorities HUD with summary, draft response, and quick actions.

### 5. Continuous Improvement

Track corrections when I undo an action. Periodically analyze corrections and propose updates to
the rules file.

## Acceptance Criteria

- Newsletters and automated notifications are archived without my involvement within 30 minutes
- High-priority emails appear in the TUI within 15 minutes with a summary
- A draft reply is created for `needs_reply` emails and visible in Apple Mail
- All automated actions are logged and reversible
- The email rules file is watched and applied without restart
- Manual corrections are recorded
