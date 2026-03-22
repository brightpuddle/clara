---
plan_recommended: false
---

# Behavior Logging Expansion

## Context

Much of my behavior is already logged, but there are gaps, and the existing data is not being
actively leveraged by Clara.

This intent has two purposes:
1. Fill logging gaps
2. Analyze existing behavioral data for insights and automation opportunities

**Dependencies:**
- FS MCP server
- LLM MCP server
- SQLite store
- ClaraBridge for macOS-specific data

## Phase 1: Audit Existing Logging

Enumerate and document what is currently logged: zsh history, zoxide, Screentime, browser history,
emails, Clara intent runs, calendar data, and more. Write the audit to a ZK vault note.

## Phase 2: Fill Logging Gaps

Add targeted logging for:
- Frontmost app, window title, and working directory snapshots
- FS MCP tool-call logging
- Clara command/session context logging

## Phase 3: Progressive Analysis

Analyze the behavioral corpus weekly for:
- Time allocation
- Workflow pain points
- Repetitive patterns
- Automation opportunities

Write the analysis to a ZK note and surface high-confidence opportunities in the TUI.

## Data Retention Policy

- High-resolution data: 90 days
- Daily summaries: 1 year
- Weekly summaries: indefinite

## Acceptance Criteria

- The logging audit note is written on first run
- 30-minute behavioral snapshots are captured and stored
- Browser history can be analyzed for recent domain usage
- A weekly analysis note is produced with concrete observations
- Old data is rolled up and pruned according to retention policy
