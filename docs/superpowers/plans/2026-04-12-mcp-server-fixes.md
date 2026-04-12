# Fix MCPServer Lifecycle and Thread Safety Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix a bug in HTTP background reconnection and add thread safety to `MCPServer`.

**Architecture:** 
1. Modify `startHTTP` to return `nil` when background reconnection is started, so `Start` doesn't cancel the context.
2. Add a `sync.RWMutex` to `MCPServer` and use it to protect `status` and `cancel` fields.

**Tech Stack:** Go, sync package.

---

### Task 1: Fix `startHTTP` reconnection bug

**Files:**
- Modify: `internal/registry/mcp_server.go`

- [ ] **Step 1: Modify `startHTTP` to return `nil` on initial failure**

We want `Start` to succeed if background reconnection is started.

```go
func (s *MCPServer) startHTTP(ctx context.Context, r *Registry) error {
	// Give the initial connection attempt a short timeout to prevent blocking startup.
	initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := s.connectHTTP(initCtx, r); err != nil {
		s.log.Warn().
			Err(err).
			Str("url", s.url).
			Dur("retry_interval", httpReconnectInterval).
			Msg("HTTP MCP server not reachable at startup; retrying in background")

		s.status = StatusConnecting
		go s.backgroundReconnect(ctx, r)
		return nil // Return nil so Start() doesn't cancel the context
	}
	s.status = StatusRunning
	return nil
}
```

- [ ] **Step 2: Update `Start` to handle the status transition correctly**

In `Start`, the defer currently might overwrite `StatusConnecting` with `StatusRunning` if `err == nil`.

```go
	defer func() {
		if err != nil {
			s.status = StatusFailed
			cancel()
		} else if s.status != StatusConnecting {
			s.status = StatusRunning
		}
	}()
```

Wait, if `startHTTP` returns `nil` and sets `status = StatusConnecting`, this defer will see `err == nil` and `s.status == StatusConnecting`, so it won't change it to `StatusRunning`. This is correct.

- [ ] **Step 3: Verify the fix with a test case (optional but recommended)**

I should check if there's an existing test for this. `internal/registry/mcp_server_test.go` seems relevant.

---

### Task 2: Add thread safety to `MCPServer`

**Files:**
- Modify: `internal/registry/mcp_server.go`

- [ ] **Step 1: Add `sync.RWMutex` to `MCPServer` struct**

```go
type MCPServer struct {
    // ... existing fields ...
    mu          sync.RWMutex
	status      ServerStatus
	cancel      context.CancelFunc
    // ...
}
```

- [ ] **Step 2: Add helper methods for status and cancel**

```go
func (s *MCPServer) Status() ServerStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *MCPServer) setStatus(status ServerStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
}
```

- [ ] **Step 3: Update all accesses to `status` and `cancel` to use the mutex**

This includes `Start`, `Stop`, `connectHTTP`, `startStdio`, and anywhere else they are used.

- [ ] **Step 4: Commit the changes**

```bash
git add internal/registry/mcp_server.go
git commit -m "fix(registry): fix HTTP reconnect loop and add thread safety to MCPServer"
```

---

### Task 3: Verification

- [ ] **Step 1: Run registry tests**

Run: `go test -v ./internal/registry/...`
Expected: PASS
