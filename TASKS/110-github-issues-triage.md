---
plan_recommended: false
---

# GitHub Issues Triage Intent

## Context

I maintain several automation projects in an enterprise GitHub account. Issues come from multiple
sources: GitHub directly, ideas from my notes, requests mentioned in Webex or email. This intent
centralizes triage, drafts responses, identifies Copilot-assignable issues, and keeps GitHub as the
single source of truth for project work.

**Dependencies:**
- GitHub MCP server configured in `config.yaml`
- LLM MCP server (task 070)
- ZK MCP server for cross-referencing relevant notes

## Workflow

### 1. Triage New Issues

For each newly opened GitHub issue:
- Classify: `bug`, `enhancement`, `docs`, `question`, `duplicate`, `wontfix`
- Assess Copilot assignability
- Draft a response
- Store the draft in `github_issue_queue`

### 2. Centralize Issues from Other Sources

When the priorities HUD surfaces a GitHub-worthy request from email or Webex, create a GitHub issue
from the content and link the source.

### 3. Present to Me via TUI

Render pending GitHub queue items as Q&A suggestions with quick actions to view, respond, assign,
close as duplicate, or snooze.

### 4. Copilot Assignment

For issues flagged `copilot_assignable`:
- Verify acceptance criteria exist
- Assign to Copilot
- Monitor for PR creation and surface the PR for review

## Acceptance Criteria

- New issues appear in the TUI queue within 30 minutes
- AI classification is correct for at least 80% of issues in testing
- Draft responses are plausible and reference repo context
- Issues flagged as `copilot_assignable` can be assigned from the TUI
- Creating an issue from an external source works end-to-end
- Actioned issues are not re-surfaced
