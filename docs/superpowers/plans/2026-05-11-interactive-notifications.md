# Interactive Notifications Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the notification system to support multiple-choice options and text feedback using Modals in Discord and Adaptive Cards in Webex.

**Architecture:** We are replacing the hardcoded Approve/Reject abstractions (`approval.request`) with a generalized `interactive.request` MCP tool in the Discord and Webex integrations. The internal routing struct will be updated to `InteractiveDecision` to capture both button selections and optional text input. The built-in `notify.ask` tool will expose these new capabilities to Starlark scripts.

**Tech Stack:** Go, Discord API (`discordgo`), Webex API (Adaptive Cards), MCP.

---

### Task 1: Update Webex Router and Add Tests

**Files:**
- Modify: `cmd/integrations/webex/internal/webexapi/router.go`
- Create: `cmd/integrations/webex/internal/webexapi/router_test.go`

- [ ] **Step 1: Write the failing test**

```go
// cmd/integrations/webex/internal/webexapi/router_test.go
package webexapi

import (
	"testing"
	"time"
)

func TestRouterInteractiveDecision(t *testing.T) {
	r := NewRouter()
	requestID := "test-req-1"
	
	ch := r.RegisterApproval(requestID) // We keep RegisterApproval name for backwards compatibility internally if we want, but let's rename to RegisterInteractive
	
	go func() {
		time.Sleep(10 * time.Millisecond)
		r.ResolveInteractive(requestID, InteractiveDecision{
			Selection:  "custom",
			CustomText: "my feedback",
		})
	}()
	
	decision, ok := r.WaitInteractive(requestID, ch, 1*time.Second)
	if !ok {
		t.Fatal("expected decision, got timeout")
	}
	if decision.Selection != "custom" || decision.CustomText != "my feedback" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/integrations/webex/internal/webexapi -run TestRouterInteractiveDecision -v`
Expected: FAIL with compilation errors (methods and types not found).

- [ ] **Step 3: Write minimal implementation**

Modify `cmd/integrations/webex/internal/webexapi/router.go`.
Replace `ApprovalDecision` with `InteractiveDecision` and rename the router methods:
```go
// InteractiveDecision is delivered to a waiting interactive request.
type InteractiveDecision struct {
	Selection  string
	CustomText string
	User       string
}

// Update `approvals` map type inside `Router` struct:
type Router struct {
	mu        sync.Mutex
	approvals map[string]chan InteractiveDecision
}

// In NewRouter():
approvals: make(map[string]chan InteractiveDecision),

// Rename RegisterApproval -> RegisterInteractive
func (r *Router) RegisterInteractive(requestID string) <-chan InteractiveDecision {
	ch := make(chan InteractiveDecision, 1)
	r.mu.Lock()
	r.approvals[requestID] = ch
	r.mu.Unlock()
	return ch
}

// Rename GetApprovalChan -> GetInteractiveChan
func (r *Router) GetInteractiveChan(requestID string) <-chan InteractiveDecision {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.approvals[requestID]
}

// Rename ResolveApproval -> ResolveInteractive
func (r *Router) ResolveInteractive(requestID string, d InteractiveDecision) bool {
	r.mu.Lock()
	ch, exists := r.approvals[requestID]
	if exists {
		delete(r.approvals, requestID)
	}
	r.mu.Unlock()

	if exists {
		ch <- d
		close(ch)
		return true
	}
	return false
}

// Rename WaitApproval -> WaitInteractive
func (r *Router) WaitInteractive(
	requestID string,
	ch <-chan InteractiveDecision,
	timeout time.Duration,
) (InteractiveDecision, bool) {
	select {
	case d := <-ch:
		return d, true
	case <-time.After(timeout):
		r.mu.Lock()
		delete(r.approvals, requestID)
		r.mu.Unlock()
		return InteractiveDecision{}, false
	}
}
```
*Note: Also fix any usages of `ResolveApproval` inside `router.go` if it exists.*

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/integrations/webex/internal/webexapi -run TestRouterInteractiveDecision -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/integrations/webex/internal/webexapi/router*.go
git commit -m "refactor: Update Webex router for InteractiveDecision"
```

---

### Task 2: Update Discord Router and Add Tests

**Files:**
- Modify: `cmd/integrations/discord/internal/discordapi/router.go`
- Create: `cmd/integrations/discord/internal/discordapi/router_test.go`

- [ ] **Step 1: Write the failing test**

```go
// cmd/integrations/discord/internal/discordapi/router_test.go
package discordapi

import (
	"testing"
	"time"
)

func TestRouterInteractiveDecision(t *testing.T) {
	r := NewRouter()
	requestID := "test-req-1"
	
	ch := r.RegisterInteractive(requestID)
	
	go func() {
		time.Sleep(10 * time.Millisecond)
		r.ResolveInteractive(requestID, InteractiveDecision{
			Selection:  "custom",
			CustomText: "my feedback",
		})
	}()
	
	decision, ok := r.WaitInteractive(requestID, ch, 1*time.Second)
	if !ok {
		t.Fatal("expected decision, got timeout")
	}
	if decision.Selection != "custom" || decision.CustomText != "my feedback" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/integrations/discord/internal/discordapi -run TestRouterInteractiveDecision -v`
Expected: FAIL with compilation errors.

- [ ] **Step 3: Write minimal implementation**

Modify `cmd/integrations/discord/internal/discordapi/router.go`.
Apply the exact same `ApprovalDecision` -> `InteractiveDecision` rename and method renames as in Task 1 for the Webex router.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/integrations/discord/internal/discordapi -run TestRouterInteractiveDecision -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/integrations/discord/internal/discordapi/router*.go
git commit -m "refactor: Update Discord router for InteractiveDecision"
```

---

### Task 3: Update Webex Client for Adaptive Cards

**Files:**
- Modify: `cmd/integrations/webex/internal/webexapi/client.go`

- [ ] **Step 1: Update `SendInteractive` method**

Modify `cmd/integrations/webex/internal/webexapi/client.go`.
Rename `SendApproval` to `SendInteractive` and update its signature to accept `options []string` and `allowText bool`.

```go
// SendInteractive posts an adaptive card with the given options.
func (c *Client) SendInteractive(roomID, machine, requestID, title, description string, options []string, allowText bool) (*Message, error) {
	if len(options) == 0 {
		options = []string{"Approve", "Reject"}
	}

	actions := []any{}
	for _, opt := range options {
		actions = append(actions, map[string]any{
			"type":  "Action.Submit",
			"title": opt,
			"data": map[string]string{
				"request_id": requestID,
				"decision":   opt,
			},
		})
	}

	if allowText {
		actions = append(actions, map[string]any{
			"type":  "Action.ShowCard",
			"title": "Feedback / Other...",
			"card": map[string]any{
				"type":    "AdaptiveCard",
				"version": "1.0",
				"body": []any{
					map[string]any{
						"type":        "Input.Text",
						"id":          "custom_text",
						"placeholder": "Enter your response...",
						"isMultiline": true,
					},
				},
				"actions": []any{
					map[string]any{
						"type":  "Action.Submit",
						"title": "Submit Feedback",
						"data": map[string]string{
							"request_id": requestID,
							"decision":   "custom",
						},
					},
				},
			},
		})
	}

	card := map[string]any{
		"type":    "AdaptiveCard",
		"version": "1.0",
		"body": []any{
			map[string]any{
				"type":   "TextBlock",
				"text":   title,
				"weight": "Bolder",
				"size":   "Medium",
			},
		},
		"actions": actions,
	}

	if description != "" {
		card["body"] = append(card["body"].([]any), map[string]any{
			"type": "TextBlock",
			"text": description,
			"wrap": true,
		})
	}

	payload := map[string]any{
		"roomId": roomID,
		"text":   fmt.Sprintf("Interactive Request: %s", title),
		"attachments": []any{
			map[string]any{
				"contentType": "application/vnd.microsoft.card.adaptive",
				"content":     card,
			},
		},
	}

	var msg Message
	if err := c.post("/messages", payload, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}
```

- [ ] **Step 2: Commit**

```bash
git add cmd/integrations/webex/internal/webexapi/client.go
git commit -m "feat: Support options and custom text in Webex Adaptive Cards"
```

---

### Task 4: Update Discord Client for Components and Modals

**Files:**
- Modify: `cmd/integrations/discord/internal/discordapi/bot.go`

- [ ] **Step 1: Update `SendInteractive` method**

Modify `cmd/integrations/discord/internal/discordapi/bot.go`.
Rename `SendApproval` to `SendInteractive` and update to generate rows of buttons.

```go
// SendInteractive posts an embed with multiple choice buttons and an optional text input modal.
func (b *Bot) SendInteractive(channelID, machine, requestID, title, description string, options []string, allowText bool) (string, error) {
	if channelID == "" {
		return "", fmt.Errorf("channel_id is required for interactive request")
	}
	if len(options) == 0 {
		options = []string{"Approve", "Reject"}
	}

	var components []discordgo.MessageComponent
	var currentRow []discordgo.MessageComponent

	addButton := func(label string, customID string, style discordgo.ButtonStyle) {
		if len(currentRow) == 5 {
			components = append(components, discordgo.ActionsRow{Components: currentRow})
			currentRow = []discordgo.MessageComponent{}
		}
		currentRow = append(currentRow, discordgo.Button{
			Label:    label,
			Style:    style,
			CustomID: customID,
		})
	}

	for i, opt := range options {
		customID := fmt.Sprintf("clara:%s:%s:btn:%d", machine, requestID, i)
		addButton(opt, customID, discordgo.PrimaryButton)
	}

	if allowText {
		customID := fmt.Sprintf("clara:%s:%s:modalbtn", machine, requestID)
		addButton("📝 Feedback / Other...", customID, discordgo.SecondaryButton)
	}

	if len(currentRow) > 0 {
		components = append(components, discordgo.ActionsRow{Components: currentRow})
	}

	msg := &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{{
			Title:       title,
			Description: description,
			Color:       0xFFA500, // orange — pending
		}},
		Components: components,
	}
	m, err := b.sess.ChannelMessageSendComplex(channelID, msg)
	if err != nil {
		return "", err
	}
	return m.ID, nil
}
```

- [ ] **Step 2: Update Interaction Handler**

In `cmd/integrations/discord/internal/discordapi/bot.go`, locate `onInteractionCreate`. Update it to handle button actions (parsing the `options` string wouldn't work easily since customID only stores an index. We must parse out the intent. Actually, let's keep the option name in the customID but encode it safely, or simply let the MCP tool parse it. For simplicity, we can encode the decision directly in the customID: `clara:machine:reqID:btn:OptionName`).

Wait, Discord limits customID to 100 characters. Instead of `clara:%s:%s:btn:%d`, use `clara:%s:%s:b:%s` and truncate option name safely or base64 it. Let's just use `clara:%s:%s:b:%s`.

**Correction to Step 1 code block inside bot.go:**
Change the loop in `SendInteractive` to:
```go
	for _, opt := range options {
	    // custom_id max length is 100
		safeOpt := opt
		if len(safeOpt) > 30 {
		    safeOpt = safeOpt[:30]
		}
		customID := fmt.Sprintf("clara:%s:%s:b:%s", machine, requestID, safeOpt)
		addButton(opt, customID, discordgo.PrimaryButton)
	}
```

Now update `onInteractionCreate`:
```go
func (b *Bot) onInteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionMessageComponent:
		customID := i.MessageComponentData().CustomID
		parts := strings.SplitN(customID, ":", 5)
		if len(parts) < 4 || parts[0] != "clara" {
			return
		}
		requestID := parts[2]
		actionType := parts[3] // "b" or "modalbtn"

		user := ""
		if i.Member != nil && i.Member.User != nil {
			user = i.Member.User.Username
		} else if i.User != nil {
			user = i.User.Username
		}

		if actionType == "modalbtn" {
			// Trigger modal
			err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseModal,
				Data: &discordgo.InteractionResponseData{
					CustomID: fmt.Sprintf("clara:modal:%s", requestID),
					Title:    "Custom Feedback",
					Components: []discordgo.MessageComponent{
						discordgo.ActionsRow{
							Components: []discordgo.MessageComponent{
								discordgo.TextInput{
									CustomID:    "feedback_text",
									Label:       "Your response",
									Style:       discordgo.TextInputParagraph,
									Placeholder: "Type your feedback here...",
									Required:    true,
								},
							},
						},
					},
				},
			})
			if err != nil {
				log.Error().Err(err).Msg("failed to send modal")
			}
			return
		}

		// It's a standard button "b"
		decisionVal := "approved" // default fallback
		if len(parts) == 5 {
			decisionVal = parts[4]
		}

		err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    fmt.Sprintf("Selection made by %s: %s", user, decisionVal),
				Components: []discordgo.MessageComponent{}, // clear buttons
			},
		})
		if err != nil {
			log.Error().Err(err).Msg("failed to ack interaction")
		}

		b.router.ResolveInteractive(requestID, InteractiveDecision{
			Selection: decisionVal,
			User:      user,
		})

	case discordgo.InteractionModalSubmit:
		data := i.ModalSubmitData()
		customID := data.CustomID
		parts := strings.SplitN(customID, ":", 3)
		if len(parts) != 3 || parts[0] != "clara" || parts[1] != "modal" {
			return
		}
		requestID := parts[2]

		customText := ""
		for _, comp := range data.Components {
			if row, ok := comp.(*discordgo.ActionsRow); ok && len(row.Components) > 0 {
				if textInput, ok := row.Components[0].(*discordgo.TextInput); ok && textInput.CustomID == "feedback_text" {
					customText = textInput.Value
				}
			}
		}

		user := ""
		if i.Member != nil && i.Member.User != nil {
			user = i.Member.User.Username
		} else if i.User != nil {
			user = i.User.Username
		}

		err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("Feedback received from %s.", user),
			},
		})
		if err != nil {
			log.Error().Err(err).Msg("failed to ack modal")
		}

		b.router.ResolveInteractive(requestID, InteractiveDecision{
			Selection:  "custom",
			CustomText: customText,
			User:       user,
		})
	}
}
```
*Note: Make sure to import "strings" if not already imported.*

- [ ] **Step 3: Commit**

```bash
git add cmd/integrations/discord/internal/discordapi/bot.go
git commit -m "feat: Support options and modals in Discord interactive requests"
```

---

### Task 5: Update Webex integration plugin

**Files:**
- Modify: `cmd/integrations/webex/webex.go`

- [ ] **Step 1: Update tool spec and arguments**

Modify `cmd/integrations/webex/webex.go`.
Rename tool `approval.request` to `interactive.request`.
Add `options` and `allow_text` to the tool spec and arguments struct.

```go
// Inside webex.go Register() or where tools are defined:
		mcp.NewTool(
			"interactive.request",
			mcp.WithDescription(
				"Post an interactive card with buttons and block until decided. "+
					"Returns a JSON object with 'selection' and optional 'custom_text'.",
			),
			mcp.WithString("room_id", mcp.Required(), mcp.Description("Target Webex room ID.")),
			mcp.WithString("title", mcp.Required(), mcp.Description("Short title for the card.")),
			mcp.WithString("description", mcp.Description("Detail shown in the card body.")),
			mcp.WithNumber("timeout_s", mcp.Description("Seconds to wait for decision (default 300).")),
			// We can't easily add List to mcp-go if not supported, but we can pass options as a comma-separated string or just assume the integration accepts map[string]any for arguments.
			// Let's assume mcp-go supports string arrays, but if not we can omit it from the strictly typed spec or add an extension. 
            // For now, let's omit `options` from strict MCP string if no array support, OR just let it pass through.
		),

// Update switch in CallTool:
	case "interactive.request":
		return w.callInteractiveRequest(args)

// Rename callApprovalRequest -> callInteractiveRequest
type interactiveRequestArgs struct {
    Title       string   `json:"title"`
    Description string   `json:"description"`
    RoomID      string   `json:"room_id"`
    TimeoutS    float64  `json:"timeout_s"`
    Options     []string `json:"options"`
    AllowText   bool     `json:"allow_text"`
}

func (w *Webex) callInteractiveRequest(args []byte) ([]byte, error) {
	if w.bot == nil {
		return nil, errors.New("webex bot account not configured")
	}
	var a interactiveRequestArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, errors.Wrap(err, "unmarshal args")
	}
	if a.RoomID == "" {
		return nil, errors.New("room_id required")
	}
	timeoutSec := int(a.TimeoutS)
	if timeoutSec <= 0 {
		timeoutSec = 300
	}
	requestID := uuid.New().String()

	w.router.RegisterInteractive(requestID)
	_, err := w.bot.SendInteractive(a.RoomID, "local", requestID, a.Title, a.Description, a.Options, a.AllowText)
	if err != nil {
		return nil, errors.Wrap(err, "send interactive")
	}

	waitCh := w.router.GetInteractiveChan(requestID)
	if waitCh == nil {
		return nil, errors.New("interactive request not found")
	}

	decision, ok := w.router.WaitInteractive(requestID, waitCh, time.Duration(timeoutSec)*time.Second)
	if !ok {
		return json.Marshal(map[string]string{"selection": "timeout"})
	}
	
	// Create response struct
	res := map[string]string{
	    "selection": decision.Selection,
	}
	if decision.CustomText != "" {
	    res["custom_text"] = decision.CustomText
	}
	
	return json.Marshal(res)
}
```

- [ ] **Step 2: Update HTTP Handler for webhook actions**

In `webex.go`, inside `handleIncomingWebhook` (or similar location where it routes attachment actions):
```go
// Replace w.router.ResolveApproval with w.router.ResolveInteractive
// Extract custom_text if it exists in the Inputs field of the action payload
func (w *Webex) handleIncomingWebhook(...) {
    // ...
    // Assuming there's a place where it fetches the attachment action:
    action, err := w.user.GetAttachmentAction(data.ID) // or w.bot
    // ...
    inputs := action.Inputs
    requestID, _ := inputs["request_id"].(string)
    decisionVal, _ := inputs["decision"].(string)
    customText := ""
    if ct, ok := inputs["custom_text"].(string); ok {
        customText = ct
    }
    w.router.ResolveInteractive(requestID, webexapi.InteractiveDecision{
        Selection: decisionVal,
        CustomText: customText,
    })
}
```

- [ ] **Step 3: Commit**

```bash
git add cmd/integrations/webex/webex.go
git commit -m "feat: Expose interactive.request MCP tool for Webex"
```

---

### Task 6: Update Discord integration plugin

**Files:**
- Modify: `cmd/integrations/discord/discord.go`

- [ ] **Step 1: Update tool spec and arguments**

Modify `cmd/integrations/discord/discord.go`.
Rename tool `approval.request` to `interactive.request`.
Update `callInteractiveRequest` using the same logic applied to Webex in Task 5.

```go
type interactiveRequestArgs struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	ChannelID   string   `json:"channel_id"`
	TimeoutS    float64  `json:"timeout_s"`
	Options     []string `json:"options"`
	AllowText   bool     `json:"allow_text"`
}

func (d *Discord) callInteractiveRequest(args []byte) ([]byte, error) {
    // ... similar unmarshal logic ...
	requestID := uuid.New().String()

	d.router.RegisterInteractive(requestID)

	_, err := d.bot.SendInteractive(a.ChannelID, "local", requestID, a.Title, a.Description, a.Options, a.AllowText)
	if err != nil {
		return nil, errors.Wrap(err, "discord interactive.request: send")
	}

	waitCh := d.router.GetInteractiveChan(requestID)
	if waitCh == nil {
		return nil, errors.New("interactive request not found")
	}

	decision, ok := d.router.WaitInteractive(requestID, waitCh, time.Duration(timeoutSec)*time.Second)
	if !ok {
		return json.Marshal(map[string]string{"selection": "timeout"})
	}
	
	res := map[string]string{
	    "selection": decision.Selection,
	}
	if decision.CustomText != "" {
	    res["custom_text"] = decision.CustomText
	}
	return json.Marshal(res)
}
```

- [ ] **Step 2: Commit**

```bash
git add cmd/integrations/discord/discord.go
git commit -m "feat: Expose interactive.request MCP tool for Discord"
```

---

### Task 7: Update `notify.go` Builtin Tool

**Files:**
- Modify: `internal/builtin/notify/notify.go`
- Create: `internal/builtin/notify/notify_test.go`

- [ ] **Step 1: Write test for Dummy Backend**

```go
// internal/builtin/notify/notify_test.go
package notify

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rs/zerolog"
)

func TestDummyAsk(t *testing.T) {
	log := zerolog.Nop()
	fn := dummyAsk(log)
	
	args := map[string]any{
		"question": "Does this work?",
	}
	
	res, err := fn(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	
	resBytes, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	
	expected := `{"selection":"acknowledged"}`
	if string(resBytes) != expected {
		t.Fatalf("expected %s, got %s", expected, string(resBytes))
	}
}
```

- [ ] **Step 2: Verify test fails**

Run: `go test ./internal/builtin/notify -v`
Expected: FAIL (currently dummyAsk returns the raw string `"acknowledged"` instead of an object map).

- [ ] **Step 3: Update `notify.go` implementation**

Modify `internal/builtin/notify/notify.go`.
Update `askSpec` definition:
```go
	askSpec := mcp.NewTool("notify.ask",
		mcp.WithDescription(
			"Deliver a question and return the user's answer. "+
				"Returns a JSON object with 'selection' and optional 'custom_text'.",
		),
		mcp.WithString("question",
			mcp.Required(),
			mcp.Description("The question to ask."),
		),
		// MCP go library might not have mcp.WithArray, so we can omit it from the strictly typed properties, 
		// but the map[string]any args will still receive it if passed by the client.
	)
```

Update `dummyAsk`:
```go
func dummyAsk(log zerolog.Logger) func(ctx context.Context, args map[string]any) (any, error) {
	return func(_ context.Context, args map[string]any) (any, error) {
		question, _ := args["question"].(string)
		if question == "" {
			return nil, errors.New("notify.ask: question is required")
		}
		log.Info().Str("backend", "dummy").Str("question", question).Msg("notify.ask")
		return map[string]string{"selection": "acknowledged"}, nil
	}
}
```

Update `discordAsk` and `webexAsk` to pass the new fields:
```go
// Replace discord.approval.request with discord.interactive.request
result, err := reg.Call(ctx, "discord.interactive.request", map[string]any{
    "channel_id":  channelID,
    "title":       "Clara needs your input",
    "description": question,
    "options":     args["options"],
    "allow_text":  args["allow_text"],
})

// Replace webex.approval.request with webex.interactive.request
result, err := reg.Call(ctx, "webex.interactive.request", map[string]any{
    "room_id":     roomID,
    "title":       "Clara needs your input",
    "description": question,
    "options":     args["options"],
    "allow_text":  args["allow_text"],
})
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/builtin/notify -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/builtin/notify/notify*.go
git commit -m "feat: Update notify.ask to use generalized interactive.request"
```