# Design Spec: Tool Namespacing and LLM JSON Decoding

This spec outlines the implementation of dual-namespacing for tools and a native `decode="json"` parameter for the `llm.generate` tool.

## 1. Tool Namespacing Logic

### 1.1 Goal
Ensure tools are accessible via both their "clean" names (e.g., `reminders.list`) and their server-prefixed names (e.g., `macos.reminders.list`) to resolve naming conflicts.

### 1.2 Proposed Changes
- **File:** `internal/registry/registry.go`
- **Method:** `registerMCPToolsLocked`
- **Logic:**
    1.  For each tool `T` from server `S`:
        a. Calculate the "clean" name: `cleanName = getFQToolName(S, T)`.
        b. Calculate the "namespaced" name: `nsName = S + "." + cleanName` (if `cleanName` is not already prefixed by `S.` or is different from `cleanName`).
        c. Register the tool under `nsName`.
        d. Attempt to register the tool under `cleanName`.
        e. If `cleanName` is already registered, log a warning and skip its registration (first server wins).
- **Naming Conventions:**
    - If `S="macos"` and `T="reminders_list"`, then `cleanName="reminders.list"` and `nsName="macos.reminders.list"`.
    - If `S="github"` and `T="list_issues"`, then `cleanName="github.list_issues"` and `nsName="github.list_issues"` (since they are identical, only one registration occurs).

### 1.3 Testing
- Add a test case to `internal/registry/aggregation_test.go` that simulates two servers attempting to register tools that map to the same "clean" name.
- Verify both tools are reachable via their namespaced names.

## 2. LLM JSON Decoding (`decode="json"`)

### 2.1 Goal
Enable the `llm.generate` tool to automatically strip markdown and parse JSON results (objects and arrays) into structured data for Starlark scripts.

### 2.2 Proposed Changes
- **File:** `internal/mcpserver/llm/providers/providers.go`
    - Rename `parseJSONObject` to `decodeJSON`.
    - Update `decodeJSON` to support:
        - Robust markdown code block stripping (handles ```json, ```, and whitespace).
        - Both JSON objects `{}` and arrays `[]`.
- **File:** `internal/mcpserver/llm/llm.go`
    - Update `parseGenerateRequest` to accept a `decode` string argument.
    - Update `handleGenerate` to use `decodeJSON` if `decode == "json"`.
    - Ensure the `GenerateResult` returned to the registry includes the `Parsed` field.

### 2.3 Tool Schema Update
Update the `llm.generate` tool definition (if any) to include the `decode` parameter in its JSON schema.

### 2.4 Testing
- Add unit tests for `decodeJSON` in `internal/mcpserver/llm/providers/providers_test.go` (if it exists) or `llm_test.go`.
- Add an integration test in `internal/mcpserver/llm/llm_test.go` verifying `llm.generate(..., decode="json")` returns a `parsed` field.

## 3. Script Update: `github_triage.star`

### 3.1 Goal
Simplify the existing triage script to use the new `decode="json"` feature.

### 3.2 Proposed Changes
- **File:** `github_triage.star`
- **Update:**
    - Call `llm.generate(prompt=prompt, category="general", decode="json")`.
    - Remove the approximately 20 lines of manual string cleaning and array parsing from the `main` function.
    - Access keywords directly from the tool's result.

## 4. Success Criteria
- `clara tool list` shows both `reminders.list` and `macos.reminders.list`.
- `clara tool call reminders.list` works.
- `clara tool call macos.reminders.list` works.
- `llm.generate(..., decode="json")` returns a parsed object/array.
- `github_triage.star` is shorter and more robust.
