---
plan_recommended: false
---

# Automation Self-Improvement Research Intent

## Context

The AI automation landscape evolves rapidly. Clara should learn from related projects, new model
capabilities, and published workflows to identify opportunities for improvement.

This is a background intent that runs weekly and produces a digest for review.

**Dependencies:**
- LLM MCP server
- ZK MCP server
- HTTP tool
- GitHub MCP

## Workflow

### 1. Track Key Projects

Monitor a watchlist file with GitHub repos, model providers, and search topics.

### 2. Competitor / Peer Analysis

Review recent features and changes in relevant automation projects and identify useful ideas for Clara.

### 3. AI Model Landscape

Track new Ollama-compatible models, Gemini API capabilities, and Copilot/OpenAI changes.

### 4. Digest Generation

Write a weekly digest note into the ZK vault and surface a concise TUI summary.

### 5. Retrospective Signal

Review Clara's own weekly activity to identify failing intents, repetitive corrections, and other
opportunities for improvement.

## Acceptance Criteria

- GitHub release monitoring detects new releases for watched repos
- The weekly digest note is written on schedule
- The TUI summary shows the count of new items
- A digest entry can trigger the idea triage workflow
- Watchlist file changes are applied on the next run
