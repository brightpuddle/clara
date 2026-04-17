# Design Spec: Reframing Clara's Identity and Platform Goals

## 1. Problem Statement
The current documentation (README, AGENTS.md, and `conductor/` files) consistently identifies Clara as a "local-first macOS" orchestrator. This no longer aligns with the project's evolving goals, which prioritize efficient and reliable orchestration regardless of resource location (local vs. remote) and aim for Linux support for the core daemon and CLI. Additionally, the project workflow contains "mobile" boilerplate that is irrelevant and potentially misleading for AI agents.

## 2. Goals
- Remove "local-first" as a primary architectural pillar.
- Shift focus to "resource-agnostic, reliable orchestration."
- Broaden platform support language to include Linux (excluding `ClaraBridge`).
- Explicitly mark Windows as a non-goal.
- Scrub irrelevant "mobile-first" workflow templates.

## 3. Proposed Changes

### 3.1 Architecture & Philosophy
- **Resource Agnostic:** Clara will consume whatever MCP services provide the necessary resources, prioritizing reliability and efficiency over location.
- **Platform Neutral Core:** The core daemon (`clara serve`), CLI, TUI, and standard MCP servers (fs, db, shell, etc.) should be presented as platform-neutral (macOS/Linux).
- **macOS Specialization:** `ClaraBridge` and related integrations (Photos, Reminders) remain macOS-specific features but are no longer the *defining* characteristic of the project.

### 3.2 File-Specific Updates

#### `AGENTS.md`
- **Revision:** Update "Project Overview" to define Clara as an "efficient, reliable agentic orchestrator."
- **Platform Support:** Explicitly state that the core (Go) components support macOS and Linux, while the Swift bridge is macOS-only.

#### `README.md`
- **Revision:** Remove "local-first" from the tagline and intro.
- **Philosophy Section:** Replace "Local-first" focus with "Efficiency & Reliability."
- **Ecosystem:** Reframe `llm` to focus on "Multiplexed access to reliable LLM providers (Gemini, local models via Ollama, etc.)" instead of emphasizing local models.

#### `conductor/product.md`
- **Vision Update:** "Clara is an efficient, reliable agentic orchestrator designed to automate digital life..."
- **Core Objectives:** Update to emphasize resource flexibility.

#### `conductor/tech-stack.md`
- **Platform:** Update to include Linux as a target for Go components.

#### `conductor/workflow.md`
- **Cleanup:** Remove the "Mobile Testing" section and all "Safari/iPhone" references in Quality Gates and Testing Requirements.

## 4. Non-Goals
- Adding Windows support.
- Refactoring `ClaraBridge` for Linux (not possible/intended).
- Removing existing macOS features (they remain, but as optional specialized integrations).
