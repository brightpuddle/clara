# Implementation Plan: Github Issues Triage Intent

## Phase 1: Foundation - Unified Search Capabilities [checkpoint: 12e77c7]
- [x] Task: Audit and update Email, Webex, and ZK MCP servers to ensure support for keyword-based search tools. 7bfadd7
- [x] Task: Implement a unified search abstraction in a shared Go utility or a helper Starlark script. e5ac73e
- [x] Task: Write unit tests for the search tools in their respective MCP servers. e5ac73e
- [x] Task: Conductor - User Manual Verification 'Foundation' (Protocol in workflow.md) 12e77c7

## Phase 2: Intent Implementation - Github Triage Workflow [checkpoint: f9f320f]
- [x] Task: Create the `github_triage.star` intent script. 1d6e7a3
- [x] Task: Implement the GitHub issue fetching and keyword extraction logic. 00f54c8
- [x] Task: Implement the cross-referencing search logic within the intent. 00f54c8
- [x] Task: Test the intent script using `clara intent start` with mock data. 3d0a6cc
- [x] Task: Conductor - User Manual Verification 'Intent Implementation' (Protocol in workflow.md) f9f320f

## Phase 3: Integration & TUI Enhancement [checkpoint: 83a00d3]
- [x] Task: Update the Clara TUI to display the triaged issues and their associated context in a multi-pane view. 83a00d3
- [x] Task: Add interactive triage actions (e.g., label, close, comment) to the TUI. 83a00d3
- [x] Task: Perform a full end-to-end test of the triage workflow. 83a00d3
- [x] Task: Conductor - User Manual Verification 'Integration & TUI Enhancement' (Protocol in workflow.md) 83a00d3
