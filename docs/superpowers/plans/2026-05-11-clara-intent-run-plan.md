# Clara Intent Run via Daemon Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Modify `clara intent run` to execute one-off scripts within an existing running daemon if one exists.

**Architecture:** Use the existing `ipc.MethodRun` message over the Unix domain socket to pass the absolute file path of the script to the running daemon. The daemon creates a one-off `orchestrator.Intent` and starts it in the background returning a `run_id`. The CLI client then tails the log for that `run_id` as it currently does. If the daemon isn't running, it falls back to current local standalone execution.

**Tech Stack:** Go (CLI + Unix Sockets)

---

### Task 1: Handle `ipc.MethodRun` in Daemon

**Files:**
- Modify: `cmd/clara/serve.go`

- [ ] **Step 1: Add `path/filepath` to imports in `serve.go`**

Add `"path/filepath"` to the import block in `cmd/clara/serve.go`.

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
```

- [ ] **Step 2: Add `case ipc.MethodRun:` block to `buildHandler`**

Inside `cmd/clara/serve.go`, locate the `switch req.Method {` inside the `buildHandler` function. Add the `case ipc.MethodRun:` block right above `case ipc.MethodStart:`.

```go
		case ipc.MethodRun:
			path, _ := req.Params["path"].(string)
			if path == "" {
				writeResp(&ipc.Response{Error: "missing path parameter"})
				return
			}
			absPath, err := filepath.Abs(path)
			if err != nil {
				writeResp(&ipc.Response{Error: err.Error()})
				return
			}
			intent := &orchestrator.Intent{
				ID:           strings.TrimSuffix(filepath.Base(absPath), filepath.Ext(absPath)),
				WorkflowType: orchestrator.WorkflowTypeNative,
				Script:       absPath,
			}
			if strings.HasSuffix(absPath, ".yaml") || strings.HasSuffix(absPath, ".yml") || strings.HasSuffix(absPath, ".json") {
				data, err := os.ReadFile(absPath)
				if err != nil {
					writeResp(&ipc.Response{Error: "read intent file: " + err.Error()})
					return
				}
				intent, err = orchestrator.ParseIntent(data)
				if err != nil {
					writeResp(&ipc.Response{Error: "parse intent: " + err.Error()})
					return
				}
			}
			
			runID := fmt.Sprintf("%s-oneoff-%d", intent.ID, time.Now().UnixNano())
			startedAt := time.Now()
			
			go runIntentInBackground(ctx, intent, runID, "main", nil, reg, db, ilog, log)
			
			writeResp(&ipc.Response{
				Message: "intent " + intent.ID + " started",
				Data: map[string]any{
					"run_id":     runID,
					"intent_id":  intent.ID,
					"started_at": startedAt.Format(time.RFC3339Nano),
				},
			})

		case ipc.MethodStart:
```

- [ ] **Step 3: Check build**

Run: `make build`
Expected: Passes without errors.

- [ ] **Step 4: Commit**

```bash
git add cmd/clara/serve.go
git commit -m "feat: handle MethodRun IPC request in daemon"
```

### Task 2: Update CLI to Send IPC Request

**Files:**
- Modify: `cmd/clara/intent.go`

- [ ] **Step 1: Rewrite `runIntentRun`**

In `cmd/clara/intent.go`, locate the `runIntentRun` function and replace it with the new implementation.

```go
func runIntentRun(cmd *cobra.Command, args []string) error {
	path := args[0]
	if isRunning(cfg.ControlSocketPath()) {
		absPath, err := filepath.Abs(path)
		if err != nil {
			return err
		}

		resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
			Method: ipc.MethodRun,
			Params: map[string]any{"path": absPath},
		})
		if err != nil {
			return err
		}

		if wantJSON() {
			prettyPrint(resp.Data)
			return nil
		}

		var runID string
		var startedAt time.Time
		if m, ok := resp.Data.(map[string]any); ok {
			runID, _ = m["run_id"].(string)
			if s, ok := m["started_at"].(string); ok {
				startedAt, _ = time.Parse(time.RFC3339Nano, s)
			}
			intentID, _ := m["intent_id"].(string)

			logPath := filepath.Join(cfg.IntentLogsDir(), intentID+".log")
			filter := intentlog.Filter{RunID: runID, Since: startedAt}
			return followSingleIntentLog(
				cmd.Context(),
				logPath,
				runID,
				filter,
				0,
				intentRunVerbose,
				cfg.DBPath(),
			)
		}
		return nil
	}

	return runOneOff(cmd.Context(), path, intentRunVerbose)
}
```

- [ ] **Step 2: Check build**

Run: `make build`
Expected: Passes without errors.

- [ ] **Step 3: Commit**

```bash
git add cmd/clara/intent.go
git commit -m "feat: route clara intent run to daemon if running"
```
