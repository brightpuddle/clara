# Design Spec: Registry Namespacing Simplification

Remove heuristic-based tool namespacing in favor of strict integration-prefixed names.

## Goals
- Simplify tool registration and lookup.
- Remove hardcoded namespace mappings (e.g., `reminders_` -> `reminders`).
- Present tools exactly as named by their integrations, prefixed by the integration name.
- Ensure the event system aligns with the simplified namespacing.

## Proposed Changes

### 1. `internal/registry/registry.go`
- **Remove `namespaceDefaults`**: Delete the hardcoded struct and variable.
- **Simplify `GetFQToolName(serverName, toolName)`**:
  - If `toolName` contains a `.`, return it as-is.
  - Otherwise, return `serverName + "." + toolName`.
  - Remove all prefix-stripping and stutter-prevention logic.
- **Deprecate/Remove Namespace Heuristics**:
  - Remove `NamespaceMeta(ns)`.
  - Remove `ServerNamespacePrefixes(serverName)`.
  - Simplify `IsKnownNamespace(name)` to check if any registered tool starts with `name + "."`.
  - Simplify `Namespaces()` to derive purely from registered tool names.

### 2. `cmd/clara/serve.go`
- **Simplify `listEventTools`**:
  - Remove "Case 2" (sub-namespace mapping).
  - Only support "Case 1" (direct server ownership).
  - Events for `macos.reminders_list` will now be part of the `macos` namespace.

### 3. `internal/registry/fqname_test.go`
- Update test cases to verify the new strict naming rules:
  - `("macos", "reminders_list")` -> `macos.reminders_list`
  - `("clara-db", "query")` -> `clara-db.query`
  - `("any", "already.qualified")` -> `already.qualified`

## Impact
- **Existing Scripts**: Starlark scripts must be updated (e.g., `reminders.list` -> `macos.reminders_list`).
- **TUI**: Tools will be grouped under their parent integration name instead of sub-namespaces.
- **Events**: Event names will now follow the parent integration's naming convention.

## Verification Plan
1. Update `fqname_test.go` and run `go test ./internal/registry/...`.
2. Verify tool listing in the TUI/CLI shows the new names.
3. Update `test_reminders.star` (if it exists) to use the new names and verify it still runs.
