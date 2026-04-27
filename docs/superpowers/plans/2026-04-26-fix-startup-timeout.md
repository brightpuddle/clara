# Fix Clara Startup Timeout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Resolve the startup hang/timeout in Clara by fixing the `fs` integration and hardening the RPC contract.

**Architecture:** 
- The `fs` integration was broken due to a missing dependency (`internal/mcpserver/markdown`) and potentially stale binary state.
- `pkg/contract/contract.go` used `new(interface{})` for empty RPC calls, which is non-idiomatic and potentially problematic for `net/rpc` serialization.
- Rebuilding integrations and cleaning up debug logs will restore stability.

**Tech Stack:** Go, `hashicorp/go-plugin`, `net/rpc`.

---

### Task 1: Harden RPC Contract

**Files:**
- Modify: `pkg/contract/contract.go`

- [ ] **Step 1: Replace `new(interface{})` with `struct{}{}` in RPC calls**

```go
func (g *IntegrationRPC) Description() (string, error) {
	var resp string
	err := g.Client.Call("Plugin.Description", struct{}{}, &resp)
	return resp, err
}

func (g *IntegrationRPC) Tools() ([]byte, error) {
	var resp []byte
	err := g.Client.Call("Plugin.Tools", struct{}{}, &resp)
	return resp, err
}
```

- [ ] **Step 2: Update RPC server methods to accept `struct{}`**

```go
func (s *IntegrationRPCServer) Description(args struct{}, resp *string) error {
	var err error
	*resp, err = s.Impl.Description()
	return err
}

func (s *IntegrationRPCServer) Tools(args struct{}, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.Tools()
	return err
}
```

- [ ] **Step 3: Commit**
`git add pkg/contract/contract.go && git commit -m "fix(contract): use struct{} for empty RPC arguments"`

---

### Task 2: Clean up Plugin Loader Logging

**Files:**
- Modify: `cmd/clara/plugins.go`

- [ ] **Step 1: Remove debug logging added during investigation**
Restore `loadIntegrationAt` to its clean state (but keep the fix for any actual bugs found).

- [ ] **Step 2: Commit**
`git add cmd/clara/plugins.go && git commit -m "chore(plugins): remove debug logging"`

---

### Task 3: Clean up FS Integration and Rebuild

**Files:**
- Modify: `cmd/integrations/fs/main.go`
- Modify: `cmd/integrations/fs/fs.go` (already partially done, but ensure clean state)

- [ ] **Step 1: Remove debug logging from `cmd/integrations/fs/main.go`**

- [ ] **Step 2: Ensure `fs.go` is clean of the broken markdown dependency**
(Double check that all markdown-related code is removed or commented out).

- [ ] **Step 3: Rebuild both `fs` and `shell` integrations**

```bash
go build -o ~/.config/clara/integrations/fs ./cmd/integrations/fs/
go build -o ~/.config/clara/integrations/shell ./cmd/integrations/shell/
```

- [ ] **Step 4: Commit**
`git add cmd/integrations/ && git commit -m "fix(fs): remove broken markdown dependency and clean up logging"`

---

### Task 4: Verification

- [ ] **Step 1: Start clara agent and verify it doesn't timeout**
Run `go run ./cmd/clara agent start` or `go run ./cmd/clara serve`.

- [ ] **Step 2: Check status**
Run `go run ./cmd/clara status`.
Expected: status: running, and tools/intents are loaded.
