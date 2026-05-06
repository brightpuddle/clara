# Webex Relay and Notification Update Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Update Webex integration and notification setup to use the `eve` relay architecture, removing the need for a local bot token and adding approval request support.

**Architecture:** Align Webex integration with the Discord relay pattern. Notifications and approval requests are routed through the `eve` relay via HTTP POST/GET. The `notify` builtin is updated to support the `webex` backend by delegating to the `webex` integration plugin.

**Tech Stack:** Go, Starlark, MCP (Model Context Protocol), SSE (Server-Sent Events).

---

### Task 1: Add Approval Request support to Webex Integration

**Files:**
- Modify: `cmd/integrations/webex/webex.go`

- [ ] **Step 1: Add `approval.request` to `Tools()`**

```go
		mcp.NewTool(
			"approval.request",
			mcp.WithDescription(
				"Post an approval embed with Approve/Reject buttons and block until decided. "+
					"Returns \"approved\", \"rejected\", or \"timeout\".",
			),
			mcp.WithString(
				"room_id",
				mcp.Required(),
				mcp.Description("Target Webex room ID."),
			),
			mcp.WithString("title", mcp.Required(), mcp.Description("Short title for the approval card.")),
			mcp.WithString("description", mcp.Description("Detail shown in the card body.")),
			mcp.WithNumber("timeout_s", mcp.Description("Seconds to wait for decision (default 300).")),
		),
```

- [ ] **Step 2: Update `CallTool` switch**

Add `case "approval.request": return w.callApprovalRequest(args)`.

- [ ] **Step 3: Implement `callApprovalRequest` and `waitDecision`**

Add the following methods and types to `webex.go`:

```go
type approvalRequestArgs struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	RoomID      string  `json:"room_id"`
	TimeoutS    float64 `json:"timeout_s"`
}

func (w *Webex) callApprovalRequest(args []byte) ([]byte, error) {
	var a approvalRequestArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, errors.Wrap(err, "webex approval.request: unmarshal")
	}
	if a.RoomID == "" {
		return nil, errors.New("webex approval.request: room_id is required")
	}
	timeoutSec := int(a.TimeoutS)
	if timeoutSec <= 0 {
		timeoutSec = 300
	}
	// Note: We use the same uuid approach as discord if needed, but let's assume eve handles it.
	// Actually discord generates it: requestID := uuid.New().String()
	// Need "github.com/google/uuid" import.
    // ... implementation logic ...
}
```

- [ ] **Step 4: Add `uuid` import to `webex.go`**

Run: `go get github.com/google/uuid` (if not already in go.mod).

- [ ] **Step 5: Verify build**

Run: `go build ./cmd/integrations/webex`

### Task 2: Update Config Structs

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Update `WebexNotifyConfig`**

Remove `BotToken` field.

```go
type WebexNotifyConfig struct {
	RoomID   string `yaml:"room_id"`
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./cmd/clara`

### Task 3: Implement Webex Backend in Notify Builtin

**Files:**
- Modify: `internal/builtin/notify/notify.go`

- [ ] **Step 1: Update `Register` switch**

Add `case "webex":` to support the webex backend.

- [ ] **Step 2: Implement `webexSend` and `webexAsk`**

```go
func webexSend(reg *registry.Registry, roomID string, log zerolog.Logger) func(ctx context.Context, args map[string]any) (any, error) {
    // calls webex.notification.send
}

func webexAsk(reg *registry.Registry, roomID string, log zerolog.Logger) func(ctx context.Context, args map[string]any) (any, error) {
    // calls webex.approval.request
}
```

- [ ] **Step 3: Verify build**

Run: `go build ./cmd/clara`

### Task 4: Update Configuration Example

**Files:**
- Modify: `config.yaml.example`

- [ ] **Step 1: Update `notify.webex` section**

Remove `bot_token`.

- [ ] **Step 2: Add `integrations.webex` section**

Include `eve_url`, `secret`, `machine`.

- [ ] **Step 3: Commit all changes**
