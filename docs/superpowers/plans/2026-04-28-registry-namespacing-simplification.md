# Registry Namespacing Simplification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove heuristic-based tool namespacing in favor of strict integration-prefixed names (e.g., `macos.reminders_list`).

**Architecture:** 
1. Simplify `GetFQToolName` in `internal/registry/registry.go` to strictly prefix the server name if no dot is present.
2. Remove hardcoded namespace mappings and metadata from the registry.
3. Update the event system in `cmd/clara/serve.go` to treat events as part of the primary integration namespace.

**Tech Stack:** Go, Starlark

---

### Task 1: Update Naming Tests

**Files:**
- Modify: `internal/registry/fqname_test.go`

- [ ] **Step 1: Update `TestGetFQToolName` with new expectations**

```go
func TestGetFQToolName(t *testing.T) {
	r := &Registry{}
	
	cases := []struct {
		server string
		tool   string
		want   string
	}{
		{"clara-db", "query", "clara-db.query"},
		{"clara-db", "db.search", "db.search"},
		{"macos", "reminders_list", "macos.reminders_list"},
		{"macos", "mail_search", "macos.mail_search"},
		{"clara-search", "mail.search", "mail.search"},
	}
	
	for _, tc := range cases {
		got := r.GetFQToolName(tc.server, tc.tool)
		if got != tc.want {
			t.Errorf("GetFQToolName(%q, %q) = %q, want %q", tc.server, tc.tool, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -v ./internal/registry/fqname_test.go`
Expected: FAIL (specifically for `macos.reminders_list` and `macos.mail_search`)

- [ ] **Step 3: Commit**

```bash
git add internal/registry/fqname_test.go
git commit -m "test: update GetFQToolName expectations"
```

---

### Task 2: Simplify Registry Naming Logic

**Files:**
- Modify: `internal/registry/registry.go`

- [ ] **Step 1: Remove `namespaceDefaults` and related types**

Remove:
- `namespaceDefaults` variable.
- Any references to it in `Namespaces()`, `IsKnownNamespace()`, `NamespaceDescription()`, `NamespaceMeta()`, `ServerNamespacePrefixes()`.

- [ ] **Step 2: Simplify `GetFQToolName`**

```go
func (r *Registry) GetFQToolName(serverName, toolName string) string {
	if strings.Contains(toolName, ".") {
		return toolName
	}
	return serverName + "." + toolName
}
```

- [ ] **Step 3: Clean up deprecated namespace methods**

Remove:
- `NamespaceMeta`
- `ServerNamespacePrefixes`

Simplify `IsKnownNamespace`:
```go
func (r *Registry) IsKnownNamespace(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if name == "tui" {
		return true
	}
	prefix := name + "."
	for t := range r.tools {
		if strings.HasPrefix(t, prefix) {
			return true
		}
	}
	for t := range r.defaultTools {
		if strings.HasPrefix(t, prefix) {
			return true
		}
	}
	return false
}
```

Simplify `Namespaces`:
```go
func (r *Registry) Namespaces() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := make(map[string]struct{})
	// Include namespaces from tool names (e.g. "mail" from "mail.search")
	for name := range r.tools {
		if dot := strings.Index(name, "."); dot != -1 {
			seen[name[:dot]] = struct{}{}
		}
	}
	for name := range r.defaultTools {
		if dot := strings.Index(name, "."); dot != -1 {
			seen[name[:dot]] = struct{}{}
		}
	}

	result := make([]string, 0, len(seen))
	for ns := range seen {
		result = append(result, ns)
	}
	sort.Strings(result)
	return result
}
```

- [ ] **Step 4: Run registry tests**

Run: `go test -v ./internal/registry/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/registry/registry.go
git commit -m "feat: simplify registry naming logic and remove hardcoded namespaces"
```

---

### Task 3: Simplify Event Tool Listing

**Files:**
- Modify: `cmd/clara/serve.go`

- [ ] **Step 1: Update `listEventTools` to remove sub-namespace logic**

```go
func listEventTools(ctx context.Context, reg *registry.Registry, namespace string) []map[string]any {
	// Case 1: direct server owns its own clara_list_events.
	directTool := namespace + ".clara_list_events"
	if _, ok := reg.Tool(directTool); ok {
		return buildEventTools(ctx, reg, directTool, namespace, "", nil)
	}

	return nil
}
```

- [ ] **Step 2: Verify `cmd/clara` builds**

Run: `go build ./cmd/clara`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add cmd/clara/serve.go
git commit -m "feat: align event tool listing with simplified namespacing"
```

---

### Task 4: Update Starlark Test Script

**Files:**
- Modify: `test_reminders.star`

- [ ] **Step 1: Update script to use new tool names**

```python
def run(ctx):
    clara.print(macos.reminders_default_list())
```

- [ ] **Step 2: Commit**

```bash
git add test_reminders.star
git commit -m "chore: update test script for new tool names"
```
