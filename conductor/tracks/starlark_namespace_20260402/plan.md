# Starlark Namespace Implementation Plan

## Objective
Deprecate the string-based `tool()` builtin, implement the `clara` protected namespace for core Starlark builtins (`task`, `describe`, `wait`, `search`, and `on`), and adopt the `clara.on(fn, **kwargs)` syntax for event triggers to prevent immediate evaluation during script compilation.

## Key Files to Modify
- `internal/orchestrator/intent_loader.go`
- `internal/interpreter/starlark.go`
- `internal/interpreter/starlark_test.go`
- `internal/supervisor/supervisor_test.go`
- `cmd/clara/agent_test.go` (and other test files utilizing legacy `tool()` calls)

## Implementation Steps

### 1. Remove Legacy `tool` Built-in
- Remove the `toolBuiltin` function from `starlarkRuntime` in `starlark.go`.
- Remove the `"tool"` entry from the `predeclared` `starlark.StringDict` in both `starlark.go` and `intent_loader.go`.

### 2. Implement Protected `clara` Namespace
- **In `starlark.go`**:
  - Remove `describe`, `task`, `on`, `wait`, and `search` from the root `predeclared` scope.
  - Create a new `starlark.Dict` containing these builtins.
  - Expose this dictionary as `"clara"` in the `predeclared` scope.
  - Update the loop over `it.reg.Namespaces()` to skip the `"clara"` string, preventing MCP servers from overwriting the core Starlark API.
- **In `intent_loader.go`**:
  - Perform the same dictionary encapsulation for `describe`, `task`, `on`, `wait`, and `search`.

### 3. Refine `clara.on` Syntax
- Modify `onBuiltin` in `intent_loader.go` to accept a `*starlark.Builtin` as its first positional argument instead of a string.
- Extract the tool name dynamically via `arg.Name()`. This allows `clara.on(fs.on_change, path="...")` to successfully extract the name `"fs.on_change"` without invoking the actual tool function during the parsing phase.
- Update `noopBuiltin` or implement a specific `onBuiltin` for `starlark.go` to gracefully ignore or parse this new structure without throwing errors.

### 4. Update Test Coverage
- Refactor all `tool("namespace.action", ...)` calls in the Go test suite to use the native dot-notation `namespace.action(...)`.
- Refactor all `task(...)`, `describe(...)`, `wait(...)`, and `search(...)` calls to prefix with `clara.`.
- Update all `on("tool.name", ...)` calls to use `clara.on(tool.name, ...)`.
- Add a specific test case in `starlark_test.go` or `intent_test.go` to verify the `clara` namespace is protected and cannot be overridden by an MCP server.

## Final Validation
- Run the full test suite (`go test ./...`) to ensure no regressions and verify the new syntax behaves identically to the old one.