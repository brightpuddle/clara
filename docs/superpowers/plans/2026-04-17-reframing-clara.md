# Reframing Clara Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor project documentation to remove "local-first" and "macOS-only" as primary design goals, shifting to "efficient, reliable, resource-agnostic orchestration" with support for Linux.

**Architecture:** Documentation-only refactoring across project-level Markdown files.

**Tech Stack:** Markdown.

---

### Task 1: Update AGENTS.md

**Files:**
- Modify: `AGENTS.md`

- [ ] **Step 1: Update Project Overview and Build commands**
Update the intro to remove "local-first" and add Linux support context.

```markdown
## Project Overview

**Clara** (`github.com/brightpuddle/clara`) is an efficient, reliable agentic orchestrator. It is a background daemon written in Go that:
...
**Architectural rule:** If a capability is available to intents, it must be delivered through MCP. Clara prioritizes the most reliable and efficient MCP services available, whether they are local or online.

- **Go 1.24+** for the daemon, CLI, and built-in MCP servers (supports macOS and Linux).
- **Swift 6.0+** for the standalone macOS MCP bridge (`swift/`).
```

- [ ] **Step 2: Verify "local-first" is removed**
Run: `grep -i "local-first" AGENTS.md`
Expected: No matches (or only in historical/contextual sections if any remain, but goal is removal).

- [ ] **Step 3: Commit**
```bash
git add AGENTS.md
git commit -m "docs: reframe Clara as resource-agnostic orchestrator in AGENTS.md"
```

---

### Task 2: Update README.md

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update Intro and Philosophy**
Remove "local-first" from the tagline and intro. Update the philosophy section to emphasize efficiency and reliability.

- [ ] **Step 2: Update Ecosystem/LLM section**
Reframe the `llm` description to include online providers.

- [ ] **Step 3: Verify changes**
Check that "local-first" is gone and Linux/Online goals are mentioned.

- [ ] **Step 4: Commit**
```bash
git add README.md
git commit -m "docs: remove local-first focus from README.md"
```

---

### Task 3: Update Conductor Product Definition

**Files:**
- Modify: `conductor/product.md`

- [ ] **Step 1: Update Vision and Objectives**
Remove "local-first" and "for macOS" from the primary identity.

- [ ] **Step 2: Update Roadmap**
Ensure the roadmap reflects the broader goal of consuming any reliable MCP server.

- [ ] **Step 3: Commit**
```bash
git add conductor/product.md
git commit -m "docs(conductor): update product vision to be resource-agnostic"
```

---

### Task 4: Update Conductor Tech Stack

**Files:**
- Modify: `conductor/tech-stack.md`

- [ ] **Step 1: Add Linux support**
Update the "Core Orchestration & Logic" section.

- [ ] **Step 2: Commit**
```bash
git add conductor/tech-stack.md
git commit -m "docs(conductor): add Linux to supported platforms in tech-stack.md"
```

---

### Task 5: Cleanup Conductor Workflow

**Files:**
- Modify: `conductor/workflow.md`

- [x] **Step 1: Remove Mobile boilerplate**
Surgically remove the "Mobile Testing" section and all "Safari/iPhone" references.

- [x] **Step 2: Verify cleanup**
Run: `grep -iE "mobile|iphone|safari" conductor/workflow.md`
Expected: No matches.

- [x] **Step 3: Commit**
```bash
git add conductor/workflow.md
git commit -m "docs(conductor): remove irrelevant mobile testing boilerplate"
```
