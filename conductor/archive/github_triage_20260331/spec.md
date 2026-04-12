# Track: Implement Github Issues Triage Intent with Keyword Search

## Overview
Automate the triage of GitHub issues by cross-referencing them with context from communication channels (Email, Webex) and personal notes (ZK). This track builds a powerful, context-aware triage workflow.

## User Story
As a developer, I want to see relevant context from my emails, chats, and notes when triaging GitHub issues, so I can make informed decisions quickly without manual searching.

## Technical Requirements
- **Starlark Intent:** A new `github_triage.star` intent.
- **Multi-Source Search:** Unified keyword search across `email`, `webex`, and `zk` MCP servers.
- **State Management:** Persist the triage status of each issue in the local SQLite store.
- **TUI Display:** Use Bubble Tea components to show the issue alongside the discovered context.
