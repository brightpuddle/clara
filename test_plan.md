# Clara V2 — End-to-End Test Plan

Track progress by marking each item: `[ ]` pending, `[x]` passed, `[!]` failed/blocked.

---

## Pre-Flight Checklist

Before running any tests, verify these conditions:

- [x] `go build ./...` passes with no errors
- [x] `go vet ./...` passes with no warnings
- [x] `~/.config/clara/config.yaml` has `integrations.llm.categories.evaluator` defined
      (rename `local` → `evaluator` if needed — see config review notes below)
- [x] Gemini model names are valid (confirm against <https://ai.google.dev/gemini-api/docs/models>)
- [x] Ollama is running: `curl http://localhost:11434/api/tags`
- [x] `qwen3:27b` model is available in Ollama

### Config Fix Required

The current config has `evaluator_category: "local"` which is ignored by the
LLM adapter. The adapter looks for categories named `evaluator`, `reasoning`,
or `fast` (in that order). Rename `local` to `evaluator`:

```yaml
integrations:
  llm:
    categories:
      evaluator:          # was "local"
        - provider: ollama
          model: qwen3.6:27b
          thinking:
            enabled: true
      fast:
        - provider: gemini
          model: gemini-3.1-flash-lite   # verify against https://ai.google.dev/gemini-api/docs/models
      reasoning:
        - provider: gemini
          model: gemini-3.5-flash        # verify against https://ai.google.dev/gemini-api/docs/models
```

---

## Test Groups

Tests are ordered so later groups depend on earlier ones passing.

---

## Group 1 — Daemon Startup & IPC (no LLM required)

### Test 1.1 — Daemon starts cleanly

```bash
clara serve
```

**Pass:** No crash, log line "clara agent starting" appears, process stays up.
**Notes:**

- [x] Pass  [ ] Fail

---

### Test 1.2 — IPC socket responds

In a second terminal while the daemon is running:

```bash
clara actuator list
clara evaluator logs -n 0
clara event logs -n 0
```

**Pass:** All three commands return without "connection refused" or error.
**Notes:**

- [x] Pass  [ ] Fail

---

### Test 1.3 — Approvals list returns empty

```bash
clara approvals list
```

**Pass:** Returns an empty list (not an error).
**Notes:**

- [x] Pass  [ ] Fail

---

## Group 2 — Event Bus & Ring Buffers (no LLM required)

### Test 2.1 — `clara request` dispatches a CloudEvent

In terminal 1:

```bash
clara event logs --follow
```

In terminal 2:

```bash
clara request "hello world"
```

**Pass:** Within ~1 second, terminal 1 shows a `com.clara.prompt` CloudEvent
entry containing the prompt text. Validates: `EmitPromptEvent` → EventBus →
`hub.PushEvent` → IPC streaming → CLI.
**Notes:**

- [x] Pass  [ ] Fail

---

### Test 2.2 — Evaluator log entry appears

In terminal 1:

```bash
clara evaluator logs --follow
```

In terminal 2:

```bash
clara request "test evaluator logging"
```

**Pass:** An evaluator log entry appears in terminal 1. With `NoopLLMClient`
(or a working LLM), any entry (even "ignore") confirms the pipeline is wired:
EventBus → Evaluator → loghub → IPC.
**Notes:**

- [x] Pass  [ ] Fail

---

### Test 2.3 — IPC streaming survives client disconnect

```bash
clara event logs --follow &
PID=$!
sleep 3
kill $PID
sleep 1
clara event logs -n 0   # daemon must still respond
```

**Pass:** The final `clara event logs -n 0` succeeds. No daemon crash. Confirms
`rawWriter` auto-cancel on disconnect works and no goroutine leak causes a hang.
**Notes:**

- [x] Pass  [ ] Fail

---

## Group 3 — LLM Evaluator Routing (requires valid LLM config)

### Test 3.1 — LLM adapter initialises

Check daemon startup logs after restarting:

```bash
clara serve 2>&1 | head -30
```

**Pass:** No "failed to create LLM adapter" warning. If you see "evaluator will
ignore all events", the LLM config is wrong — go back to the pre-flight checklist.
**Notes:**

- [x] Pass  [ ] Fail

---

### Test 3.2 — LLM returns a decision for a prompt event

```bash
clara evaluator logs --follow &
clara request "what is the weather in San Francisco"
```

Wait up to 30 seconds (Ollama can be slow).

**Pass:** Evaluator log shows a decision with `action` of `invoke`, `build`, or
`ignore` — any structured response confirms the full LLM round-trip works.
**Notes:**

- [x] Pass  [ ] Fail

---

### Test 3.3 — Heuristic fast-path cache

Send the same prompt type twice with a short gap:

```bash
clara request "hello"
sleep 2
clara request "hello"
clara evaluator logs -n 10
```

**Pass:** The second entry shows `fast-path heuristic hit` in the evaluator log,
indicating the LLM was not called again. Confirms the `heuristics` map TTL
caching works.
**Notes:**

- [x] Pass  [ ] Fail

---

## Group 4 — Actuator Execution (requires a pre-built actuator binary)

### Build a minimal test actuator

Create this file at a temporary location, e.g. `/tmp/echo-actuator/main.go`:

```go
package main

import (
    "context"
    "fmt"

    "github.com/brightpuddle/clara/pkg/sdk"
)

type EchoActuator struct{}

func (e *EchoActuator) Manifest() sdk.ActuatorManifest {
    return sdk.ActuatorManifest{
        ID:          "echo",
        Description: "Echoes the event type back to logs",
    }
}

func (e *EchoActuator) Execute(_ context.Context, ev sdk.Event) (sdk.Result, error) {
    return sdk.Result{
        Success: true,
        Output:  fmt.Sprintf("echo: received event type=%s id=%s", ev.Type, ev.ID),
    }, nil
}

func main() {
    sdk.Serve(&EchoActuator{})
}
```

Build and install it:

```bash
cd /tmp/echo-actuator
go mod init echo
go mod edit -require github.com/brightpuddle/clara@v0.0.0
go mod edit -replace github.com/brightpuddle/clara=/Users/nathan/src/clara/v2
go mod tidy
go build -o echo .
mkdir -p ~/.local/share/clara/bin
cp echo ~/.local/share/clara/bin/echo
```

---

### Test 4.1 — `clara actuator list` shows the binary

```bash
clara actuator list
```

**Pass:** The `echo` actuator appears in the list.
**Notes:**

- [x] Pass  [ ] Fail

---

### Test 4.2 — `clara actuator run` executes the binary

```bash
clara actuator run echo
clara actuator logs echo -n 5
```

**Pass:** Actuator logs show `execution succeeded` with output containing
`echo: received event type=`. Confirms the full gRPC subprocess path:
`executeActuator` → `go-plugin` → `sdk.Actuator.Execute` → result logged.
**Notes:**

- [x] Pass  [ ] Fail

---

### Test 4.3 — LLM-directed actuator invocation

Register the heuristic manually so the LLM isn't needed for this step:

```bash
# Send a request and check if LLM routes it to the echo actuator.
# You may need to prompt specifically: "run the echo actuator"
clara request "run the echo actuator"
sleep 10
clara actuator logs echo -n 5
```

**Pass:** Actuator logs show the echo actuator was invoked in response to the
request, confirming the full Event → Evaluator → executeActuator path.
**Notes:**

- [ ] Pass  [ ] Fail

---

## Group 5 — HITL Approval Queue

### Test 5.1 — Submit and list an approval

The `ApprovalStore` can be tested directly via the IPC methods. Submit a fake
approval by triggering an event that requires it (or use a test actuator that
calls `approvals.Submit`). As a simpler alternative, verify the CLI plumbing:

```bash
clara approvals list    # expect empty list, not error
```

**Pass:** Returns `[]` or empty list without error.
**Notes:**

- [ ] Pass  [ ] Fail

---

### Test 5.2 — Full approval flow (manual)

This requires a wired HITL trigger. Implement a test actuator that declares
no capabilities but attempts a shell call, or manually submit an approval via
the store. Then:

```bash
clara approvals list              # shows pending item
clara approvals show <id>         # shows context and options
clara approvals decide <id> 1     # submits decision
clara approvals list              # item is gone
```

**Pass:** All four commands succeed and the item is removed after `decide`.
**Notes:**

- [ ] Pass  [ ] Fail

---

## Group 6 — Builder Mode / Self-Modification (requires working LLM + builder)

This is the most critical test — validates the complete self-modifying loop.

### Pre-flight for Group 6

- [ ] Daemon started with a working LLM config (Group 3 tests passed)
- [ ] `CLARA_REPO_ROOT` set, or daemon running from within the repo, or the
      executable auto-resolve found the repo root (check startup logs for
      "failed to resolve clara module root" — if present, set the env var)
- [ ] `~/.local/share/clara/workspace/` directory exists (created automatically
      by `NewBuilder`, but verify)

### Test 6.1 — Builder compiles a new actuator

```bash
clara evaluator logs --follow &
clara request "create an actuator that logs the current time every time it receives an event"
```

Wait up to 2 minutes (LLM codegen + compilation).

**Pass (in order):**

1. Evaluator log shows `LLM: builder mode` with an `actuator_id`.
2. Evaluator log shows `actuator compiled` with a `binary` path.
3. Binary appears in `~/.local/share/clara/bin/`.
4. Evaluator log shows the new actuator being invoked immediately.

```bash
ls ~/.local/share/clara/bin/
ls ~/.local/share/clara/workspace/
```

**Notes:**

- [ ] Pass  [ ] Fail

---

### Test 6.2 — Compiled actuator is invoked on subsequent events

After Test 6.1 succeeds and the heuristic is cached:

```bash
clara request "create an actuator that logs the current time every time it receives an event"
clara evaluator logs -n 5
```

**Pass:** Second request hits the fast-path heuristic and goes directly to
`executeActuator` without another LLM call or compilation. Confirms the
build → register heuristic → invoke loop is complete.
**Notes:**

- [ ] Pass  [ ] Fail

---

## Notes & Issues Log

Use this section to record failures and fixes as you work through the tests.

| Test | Status | Issue | Fix Applied |
|------|--------|-------|-------------|
|      |        |       |             |

---

## Reference: Key File Locations

| Item | Path |
|------|------|
| Actuator binaries | `~/.local/share/clara/bin/` |
| Builder workspace | `~/.local/share/clara/workspace/` |
| Daemon log | `~/.local/share/clara/clara.log` |
| IPC socket | `~/.local/share/clara/control.sock` (check `cfg.ControlSocketPath()`) |
| Config | `~/.config/clara/config.yaml` |
| Env var override | `CLARA_REPO_ROOT=/Users/nathan/src/clara/v2` |
