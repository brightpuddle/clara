# Clara Intent Run Design

## Overview
Currently, `clara intent run` (and its alias `clara run`) spins up a new temporary local context (registry, db, supervisor) to execute a one-off intent script. This design modifies the command to check for a running Clara daemon, and if one exists, route the execution to the daemon so that it runs within the established environment (with all loaded plugins, MCP servers, etc.). If the daemon is not running, it falls back to the current standalone execution approach.

## Components

### CLI Modifications (`cmd/clara/intent.go` and `cmd/clara/main.go`)
1. In `runIntentRun`, add a check: `isRunning(cfg.ControlSocketPath())`.
2. If running, convert the intent file path to an absolute path using `filepath.Abs`.
3. Construct an IPC request with method `ipc.MethodRun` and parameter `path: <absolute_path>`.
4. Call `sendRequest`.
5. The response will contain the `run_id` and `started_at` in its `Data` field.
6. Use `followSingleIntentLog` to stream the output for the given `run_id`, just like `runOneOff` currently does.

### Daemon Modifications (`cmd/clara/serve.go`)
1. In `buildHandler`, add a `case ipc.MethodRun:` block to handle the IPC request.
2. The handler will:
   - Extract the `path` parameter.
   - If missing, return an error.
   - Build the `orchestrator.Intent` identically to how `runOneOff` does it (supporting both YAML/JSON parse and fallback Native script).
   - Generate a new `runID`.
   - Call `go runIntentInBackground(ctx, intent, runID, "main", nil, reg, db, ilog, log)`.
   - Return a successful response containing `run_id`, `intent_id`, and `started_at`.

## Error Handling
- If `isRunning` returns true but the IPC request fails, it will report the error rather than silently falling back to local execution.
- If the file path passed by the user is invalid, either `filepath.Abs` will fail locally, or the daemon will fail when attempting to read the file/build the intent. Both will be bubbled up to the user via the IPC error response.

## Testing Strategy
- The daemon logic can be tested by making an IPC request while a test agent is running.
- The CLI logic can be manually verified by starting an agent and running a script, then killing the agent and running the same script to verify the fallback.