---
plan_recommended: false
---

# Workflow Self-Improvement Intent

## Context

I have a highly optimized, terminal-centric workflow. My config is in dotfiles managed by chezmoi.
zoxide records folder usage; zsh records command history; Screentime has app usage data.

This intent continuously analyzes my workflow patterns and proactively identifies improvements:
better tooling, configuration fixes, new capabilities I'm not using.

**Dependencies:**
- FS MCP server
- LLM MCP server (task 070)
- ZK MCP server

## Workflow

### 1. Behavior Analysis

Analyze:
- zsh history for alias/function/script opportunities and repeated failures
- zoxide data for workspace shortcut opportunities
- Homebrew installed/outdated/unused packages
- chezmoi-managed dotfiles for outdated or conflicting config

### 2. Generate Improvement Report

Use AI to group suggestions by category and write a note into the ZK vault.

### 3. Interactive Approval in TUI

Surface suggestions one-by-one with quick actions to apply, inspect, skip, or suppress.

### 4. Staying Current

Track releases of important tools and ecosystem changes relevant to my workflow.

## Acceptance Criteria

- zsh history analysis identifies at least one real aliasing opportunity
- Outdated Homebrew formulae appear in the suggestion queue
- A config fix can be applied via the TUI and reflected in the managed dotfile
- Suppressed suggestions do not reappear
- Weekly report note is written to the ZK vault
