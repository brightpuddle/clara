---
plan_recommended: false
---

# Filesystem Organization Intent

## Context

My filesystem has a strong organizational system for code, work, and personal files, but things
scatter over time. This intent progressively organizes my filesystem by learning and applying my
existing organizational heuristics.

**Dependencies:**
- FS MCP server
- LLM MCP server (task 070)
- Clara's sqlite-vec

## Philosophy

1. **AI determines heuristics once**, then rules are applied deterministically
2. **AI handles edge cases**
3. **All moves are logged** and reversible
4. **Incremental and progressive**

## Workflow

### Phase 0: Heuristic Generation

Analyze my existing filesystem structure and generate a rules file at
`~/.config/clara/filesystem-rules.md`.

### Phase 1: Rule-Based Classification

For each file in scan zones, match against compiled rules and auto-move high-confidence matches.

### Phase 2: AI-Based Classification

For unmatched files, use AI to suggest a destination and queue it for review.

### Phase 3: User Review Queue

Surface low-confidence moves in the TUI with suggested destinations and quick actions.

### Phase 4: Stale Content

Escalate old files in scan zones that remain unorganized.

## Acceptance Criteria

- First run generates a sensible rules file
- High-confidence rule matches are moved automatically and logged
- Low-confidence files appear in the review queue
- Backout of recent moves is possible
- `max_moves_per_run` is respected
- Rule file changes are applied without restart
