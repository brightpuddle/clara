# Fix Intent Start/Logs Inconsistencies Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the "No active runs" message when following logs of a fast-finishing intent, and ensure `intent start` only follows logs when `-f` is passed.

**Architecture:**
- Add `LatestRunState(intentID)` to the store to retrieve the most recent run regardless of status.
- Update `intentWatchPrinter.printCurrentStates` to fall back to the latest run if no active runs exist.
- Correct `runIntentStart` to respect the `intentStartFollow` flag and not implicitly follow on verbose.

**Tech Stack:** Go (Cobra CLI), SQLite

---

### Task 1: Add `LatestRunState` to Store

**Files:**
- Modify: `internal/store/store.go`

- [ ] **Step 1: Implement `LatestRunState`**

Add this method to `internal/store/store.go`:

```go
func (s *Store) LatestRunState(ctx context.Context, intentID string) (*RunState, error) {
	query := `
		SELECT id, intent_id, state, status, error, started_at, updated_at, finished_at
		     , workflow_type, entrypoint, script_source, wait_name, wait_args_json
		FROM intent_runs
		WHERE 1=1
	`
	args := []any{}
	if intentID != "" {
		query += ` AND intent_id = ?`
		args = append(args, intentID)
	}
	query += ` ORDER BY started_at DESC LIMIT 1`

	var (
		state        RunState
		waitArgsJSON string
	)
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&state.RunID,
		&state.IntentID,
		&state.State,
		&state.Status,
		&state.Error,
		&state.StartedAt,
		&state.UpdatedAt,
		&state.FinishedAt,
		&state.WorkflowType,
		&state.Entrypoint,
		&state.ScriptSource,
		&state.WaitName,
		&waitArgsJSON,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "query latest run state")
	}
	if err := json.Unmarshal([]byte(waitArgsJSON), &state.WaitArgs); err != nil {
		return nil, errors.Wrap(err, "decode wait args")
	}
	return &state, nil
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/store/store.go
git commit -m "feat(store): add LatestRunState method"
```

### Task 2: Update `intentWatchPrinter` to show latest run

**Files:**
- Modify: `cmd/clara/intent.go`

- [ ] **Step 1: Update `printCurrentStates`**

Modify `printCurrentStates` in `cmd/clara/intent.go` to use `LatestRunState` if `states` is empty.

```go
func (p *intentWatchPrinter) printCurrentStates(
	ctx context.Context,
	db *store.Store,
	intentID string,
) error {
	states, err := db.ActiveRunStates(ctx, intentID)
	if err != nil {
		return errors.Wrap(err, "load active intent states")
	}

	p.printRule()
	if len(states) == 0 {
		// Fallback to latest run for this intent if specified.
		if intentID != "" {
			latest, err := db.LatestRunState(ctx, intentID)
			if err != nil {
				return err
			}
			if latest != nil {
				fmt.Printf("%s %s %s\n", p.theme.Dimmed("intent:"), p.paintIntentID(intentID), p.theme.Dimmed("(latest run)"))
				p.lastState[latest.RunID] = latest.State
				p.printStateSnapshot(*latest)
				p.printRule()
				return nil
			}
		}

		if intentID == "" {
			fmt.Println(p.theme.Dimmed("No active intents. Waiting for events..."))
		} else {
			fmt.Printf("%s %s\n", p.theme.Dimmed("intent:"), p.paintIntentID(intentID))
			fmt.Println(p.theme.Dimmed("No active runs."))
		}
		p.printRule()
		return nil
	}
    // ... rest of method
}
```

- [ ] **Step 2: Verify with `hello` intent**

Run: `clara intent start hello -f`
Expected: Should show the "finished" state of the run instead of "No active runs".

- [ ] **Step 3: Commit**

```bash
git add cmd/clara/intent.go
git commit -m "fix(cli): show latest run state in intent logs/start -f"
```

### Task 3: Fix `intent start` log following behavior

**Files:**
- Modify: `cmd/clara/intent.go`

- [ ] **Step 1: Update `runIntentStart` and `init`**

Modify `runIntentStart` to only follow if `intentStartFollow` is true. `intentStartVerbose` should no longer imply `-f` if the user wants strictly separate control, OR we keep the implication but fix the description.

Actually, the user said:
"Running `clara intent start hello` without the `-f` flag should complete the run and then exit. Only if I pass the `-f` flag should it continue to monitor the logs. Right now, in both cases it follows the logs."

Wait, my manual test showed `clara intent start hello` DOES terminate.

Let's re-read `runIntentStart`:
```go
	if intentStartFollow || intentStartVerbose {
		return followIntentEvents(cmd.Context(), intentID, intentStartVerbose)
	}
```

If `intentStartVerbose` is true, it follows.
The `init` function says:
```go
	intentStartCmd.Flags().
		BoolVarP(&intentStartVerbose, "verbose", "v", false, "show full tool args/results (implies -f)")
```

If the user wants strictly `-f` for following, we should change this.

- [ ] **Step 2: Change `intentStartVerbose` to not imply `-f`**

In `init()`:
```go
	intentStartCmd.Flags().
		BoolVarP(&intentStartVerbose, "verbose", "v", false, "show full tool args/results")
```

In `runIntentStart()`:
```go
	if intentStartFollow {
		return followIntentEvents(cmd.Context(), intentID, intentStartVerbose)
	}
```

- [ ] **Step 3: Verify termination**

Run: `clara intent start hello -v`
Expected: Should start, print message, and exit (not follow logs).

- [ ] **Step 4: Commit**

```bash
git add cmd/clara/intent.go
git commit -m "fix(cli): intent start -v no longer implies -f"
```
