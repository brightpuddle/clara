---
plan_recommended: false
---

# Automation Idea Triage Workflow

## Context

As I come up with new automation ideas, I want a lightweight way to capture them and have Clara
translate them into concrete, actionable plans within the context of my existing ecosystem.

The output of this workflow is a new task file in `TASKS/` or an update to an existing one.

**Dependencies:**
- ZK MCP server
- LLM MCP server
- FS MCP server
- Full context of Clara's capabilities and existing tasks

## Workflow

### 1. Idea Capture

Ideas can be submitted via:
- TUI prompt
- A watched ZK note such as `inbox/automation-ideas.md`
- A web UI command for Alex

### 2. Contextualization

Gather context from:
- `AGENTS.md`
- `README.md`
- existing `TASKS/` files
- ZK notes

### 3. Guided Q&A (Interactive)

Run a short Q&A in the TUI or web UI to flesh out missing details and constraints.

### 4. Task File Generation

Generate a properly formatted `TASKS/*.md` file with correct numbering, frontmatter, and content.

### 5. Deduplication Check

Check if the idea already exists in `TASKS/` and offer to update instead of duplicating.

## Acceptance Criteria

- Submitting an idea via the TUI triggers the Q&A flow
- The watch file is monitored and new ideas trigger triage automatically
- Generated task files follow the correct format
- Duplicate detection finds existing related tasks when applicable
- The generated task is placed appropriately in priority order
