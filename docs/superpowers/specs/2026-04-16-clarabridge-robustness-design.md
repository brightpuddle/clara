# Design: ClaraBridge & Agent Robustness

This document outlines the architectural improvements to prevent hangs, deadlocks, and cascading failures in the Clara agent and its Swift-based bridge.

## 1. Problem Statement
The current system suffered a deadlock due to a "cascade of filled pipes":
1. The `clara serve` daemon, running as a LaunchAgent without output redirection, filled the system's 64KB buffer for its own stdout/stderr.
2. The daemon hung on its next attempt to log, stopping its internal event loop.
3. Because the daemon stopped, it ceased reading from the `ClaraBridge` subprocess's pipes.
4. `ClaraBridge` filled its own 64KB Stderr pipe.
5. `ClaraBridge` hung on its next log attempt, which occurred on the `@MainActor`, effectively deadlocking the Swift process.
6. The CLI and all tasks hung indefinitely because no timeouts were enforced on tool calls.

## 2. Proposed Architectural Changes

### 2.1. Go Daemon (Robustness)

#### A. LaunchAgent Log Redirection
**Action:** Update `com.brightpuddle.clara.agent.plist` to include `StandardOutPath` and `StandardErrorPath`.
**Goal:** Prevent the daemon from hanging on `write` to stdout/stderr when the system buffer fills.

#### B. MCP Call Timeouts
**Action:** Implement a mandatory timeout (default 30s) for all MCP tool calls in `internal/registry/registry.go`.
**Goal:** Ensure that a hanging MCP server doesn't block a Starlark task or the CLI indefinitely.

#### C. Robust Stderr Drainage
**Action:** Modify `MCPServer.pipeStderr` to ensure it continues draining even if the logger is slow or blocked (e.g., use a buffered channel or a dropping policy).
**Goal:** Prevent a slow logger from causing a deadlock in the subprocess.

#### D. MCP Supervisor (Watchdog)
**Action:** Add a background monitor to the `Registry` that periodically "pings" active MCP servers. If a server is unresponsive for X seconds, restart it.
**Goal:** Automated recovery from hangs.

### 2.2. Swift Bridge (Defensive Programming)

#### A. MainActor Isolation
**Action:** Refactor `BridgeTools.swift` to ensure that all macOS API calls (EventKit, Photos, AppleScript) are performed in `Task.detached` or on a background actor.
**Goal:** Prevent a single slow or deadlocked macOS API call from blocking the entire bridge's command processing.

#### B. Non-blocking Background Logging
**Action:** Implement a dedicated serial dispatch queue for `ClaraLogger`.
**Goal:** Ensure that logging to Stderr or a file never blocks the `@MainActor`.

#### C. Notification Debouncing
**Action:** Add a short debounce (e.g., 500ms) to EventKit notifications before emitting events to the Go daemon.
**Goal:** Reduce I/O pressure from chatty macOS frameworks.

## 3. Implementation Plan

1. **Phase 1: Immediate Recovery & Infrastructure Fix**
   - Update LaunchAgent plist and install it.
   - Restart the agent.

2. **Phase 2: Swift Bridge Hardening**
   - Refactor logger to be background-threaded.
   - Move blocking calls off `@MainActor`.

3. **Phase 3: Go Daemon Hardening**
   - Implement tool call timeouts.
   - Improve pipe drainage robustness.

4. **Phase 4: Optimization**
   - Add debouncing to bridge events.
   - Optimize Starlark sync tasks to be less aggressive.

## 4. Verification Strategy
- **Simulated Hang:** Artificially block the `@MainActor` in Swift and verify that Go times out the request and (eventually) restarts the bridge.
- **Pipe Fill Test:** Log 1MB of data from the bridge and verify that Go drains it without blocking.
