# Tool Namespacing and LLM JSON Decoding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement dual namespacing for tools (clean and server-prefixed) and add a native `decode="json"` parameter to the `llm.generate` tool, then update the `github_triage.star` script.

**Architecture:** 
1. Modify `internal/registry/registry.go` to register each tool twice: once as its "clean" name (e.g. `reminders.list`) and once as its "server-prefixed" name (e.g. `macos.reminders.list`). 
2. Update `internal/mcpserver/llm/providers/providers.go` with a robust `decodeJSON` function.
3. Update `internal/mcpserver/llm/llm.go` to support the `decode` parameter in the `generate` tool.
4. Update `github_triage.star` to use the new feature.

**Tech Stack:** Go, Starlark, MCP.

---

### Task 1: Dual Namespacing in Registry

**Files:**
- Modify: `internal/registry/registry.go`
- Test: `internal/registry/aggregation_test.go`

- [ ] **Step 1: Write failing test for dual namespacing and conflict**

```go
func TestDualNamespacingAndConflict(t *testing.T) {
	reg := New(zerolog.Nop())
	
	// Server 1 (macos) registers reminders_list -> reminders.list
	reg.Register("reminders.list", func(ctx context.Context, args map[string]any) (any, error) {
		return "macos reminders", nil
	})
    // We expect it to also be registered as macos.reminders.list later.

	// Server 2 (ios) registers reminders_list -> reminders.list
    // Clean name should conflict (macos won), but ios.reminders.list should work.
	reg.Register("ios.reminders.list", func(ctx context.Context, args map[string]any) (any, error) {
		return "ios reminders", nil
	})

	if !reg.Has("reminders.list") { t.Error("expected reminders.list") }
	if !reg.Has("ios.reminders.list") { t.Error("expected ios.reminders.list") }
    // After implementation, we'll check for macos.reminders.list too.
}
```

- [ ] **Step 2: Run test to verify current state**

Run: `go test -v internal/registry/aggregation_test.go`
Expected: PASS (but doesn't check for the new requirement yet)

- [ ] **Step 3: Update `registerMCPToolsLocked` to implement dual registration**

Modify `internal/registry/registry.go`:
```go
func (r *Registry) registerMCPToolsLocked(
	serverName string,
	mcpClient *client.Client,
	tools []mcp.Tool,
) error {
	for _, tool := range tools {
		cleanName := r.getFQToolName(serverName, tool.Name)
		nsName := serverName + "." + cleanName
		if strings.HasPrefix(cleanName, serverName+".") {
			nsName = cleanName
		}

		mcpToolName := tool.Name
		toolFn := func(ctx context.Context, args map[string]any) (any, error) {
			req := mcp.CallToolRequest{}
			req.Params.Name = mcpToolName
			req.Params.Arguments = args
			res, err := mcpClient.CallTool(ctx, req)
			if err != nil {
				return nil, errors.Wrapf(err, "call MCP tool %q", mcpToolName)
			}
			return normalizeCallToolResult(mcpToolName, res)
		}

		spec := tool
		spec.Name = cleanName
		
		// 1. Register under namespaced name (always succeeds for this server)
		r.tools[nsName] = toolFn
		r.specs[nsName] = spec
		if spec.Description != "" {
			r.descriptions[nsName] = spec.Description
		}

		// 2. Register under clean name (first server wins)
		if nsName != cleanName {
			if _, exists := r.tools[cleanName]; !exists {
				r.tools[cleanName] = toolFn
				r.specs[cleanName] = spec
				if spec.Description != "" {
					r.descriptions[cleanName] = spec.Description
				}
			} else {
				r.log.Warn().Str("server", serverName).Str("tool", cleanName).Msg("clean tool name already registered, skipping alias")
			}
		}
	}
	r.log.Debug().Str("server", serverName).Int("count", len(tools)).Msg("MCP tools registered")
	return nil
}
```

- [ ] **Step 4: Update `TestDualNamespacingAndConflict` to verify both names**

```go
func TestDualNamespacingAndConflict(t *testing.T) {
	// ... (register tools as if they came from RegisterConnectedClient)
    // Actually, I should test RegisterConnectedClient or mock the tools loop.
}
```
*Wait, I'll just update `registerMCPToolsLocked` and then verify via a new test.*

- [ ] **Step 5: Run tests and commit**

Run: `go test -v ./internal/registry/...`
Expected: PASS

---

### Task 2: Enhanced JSON Decoding in LLM Providers

**Files:**
- Modify: `internal/mcpserver/llm/providers/providers.go`

- [ ] **Step 1: Update `decodeJSON` in `providers.go`**

```go
func decodeJSON(text string) any {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}

	// Strip markdown code blocks if present
	if strings.Contains(trimmed, "```") {
		// More robust stripping: find the first ``` and the last ```
		start := strings.Index(trimmed, "```")
		if start != -1 {
			// Find the end of the opening backticks line
			newline := strings.Index(trimmed[start:], "\n")
			if newline != -1 {
				contentStart := start + newline + 1
				end := strings.LastIndex(trimmed, "```")
				if end > contentStart {
					trimmed = strings.TrimSpace(trimmed[contentStart:end])
				}
			} else {
				// No newline, just strip backticks
				trimmed = strings.TrimSpace(strings.ReplaceAll(trimmed, "```", ""))
			}
		}
	}
    // Remove individual backticks
    trimmed = strings.Trim(trimmed, "`")
    trimmed = strings.TrimSpace(trimmed)

	if !((strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
		(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]"))) {
		return nil
	}

	var value any
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return nil
	}
	return value
}
```

- [ ] **Step 2: Commit changes to `providers.go`**

Run: `go test ./internal/mcpserver/llm/providers/...` (if tests exist)
Commit: `git add internal/mcpserver/llm/providers/providers.go && git commit -m "feat: enhance json decoding for llm results"`

---

### Task 3: Support `decode` parameter in `llm.generate`

**Files:**
- Modify: `internal/mcpserver/llm/llm.go`
- Test: `internal/mcpserver/llm/llm_test.go`

- [ ] **Step 1: Update `parseGenerateRequest` to handle `decode`**

```go
func parseGenerateRequest(args map[string]any, requireImages bool) (providers.GenerateRequest, error) {
    // ...
	call := providers.GenerateRequest{
        // ...
		Decode:   stringArg(args, "decode"),
	}
    // ...
}
```
*Note: I need to add `Decode` field to `providers.GenerateRequest` first.*

- [ ] **Step 2: Add `Decode` field to `providers.GenerateRequest`**

File: `internal/mcpserver/llm/providers/providers.go`
```go
type GenerateRequest struct {
	// ...
	Decode          string
}
```

- [ ] **Step 3: Update `handleGenerate` to use `decodeJSON`**

```go
func (s *Service) handleGenerate(...) {
    // ...
    result, err := provider.Generate(ctx, callCopy)
    if err == nil {
        if call.Decode == "json" {
            result.Parsed = decodeJSON(result.Text)
        }
        return mcp.NewToolResultStructuredOnly(result), nil
    }
    // ...
}
```

- [ ] **Step 4: Run tests and commit**

Run: `go test -v ./internal/mcpserver/llm/...`
Expected: PASS

---

### Task 4: Update `github_triage.star` script

**Files:**
- Modify: `github_triage.star`

- [ ] **Step 1: Use `decode="json"` in `extract_keywords`**

```python
def extract_keywords(title, body):
    # ...
    result = llm.generate(prompt = prompt, category = "general", decode = "json")
    return result.get("parsed", [])
```

- [ ] **Step 2: Simplify `main` function**

Remove the manual cleaning logic and use the parsed keywords directly.

- [ ] **Step 3: Run script to verify (if possible)**

Run: `clara run github_triage.star`
Expected: Success

- [ ] **Step 4: Commit and finalize**

Commit: `git commit -m "feat: simplify github triage script using native llm json decoding"`
