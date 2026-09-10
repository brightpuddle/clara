package trigger

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog/log"
)

// Runner is responsible for executing external scripts and recording output.
type Runner struct {
	defaultTimeout time.Duration
}

// NewRunner creates a new script execution runner.
func NewRunner(defaultTimeout time.Duration) *Runner {
	if defaultTimeout <= 0 {
		defaultTimeout = 60 * time.Second
	}
	return &Runner{
		defaultTimeout: defaultTimeout,
	}
}

// GenerateRunID creates a unique identifier for a run.
func GenerateRunID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("run-%s-%s", time.Now().Format("20060102-150405"), hex.EncodeToString(b))
}

// Execute runs the action for a given trigger and event.
// Following BEAM architecture, it does not return an error for script failure;
// instead, it records the failure in the RunRecord.
func (r *Runner) Execute(ctx context.Context, def Definition, eventData any) *RunRecord {
	startedAt := time.Now()
	runID := GenerateRunID()

	record := &RunRecord{
		ID:          runID,
		TriggerID:   def.ID,
		TriggerType: def.Type,
		Status:      StatusRunning,
		StartedAt:   startedAt,
	}

	// Serialize event data
	var eventJSON []byte
	var eventID, eventType string
	if eventData != nil {
		if raw, err := json.Marshal(eventData); err == nil {
			eventJSON = raw
			record.EventData = string(raw)
		}
		if em, ok := eventData.(map[string]any); ok {
			if id, ok := em["id"].(string); ok {
				eventID = id
			}
			if t, ok := em["type"].(string); ok {
				eventType = t
			}
		}
	}

	timeout := def.Action.Timeout
	if timeout <= 0 {
		timeout = r.defaultTimeout
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmdArgs := append([]string{}, def.Action.Args...)

	passMode := def.Action.PassEvent
	if passMode == "" {
		passMode = PassEventStdin
	}

	if passMode == PassEventArg && len(eventJSON) > 0 {
		cmdArgs = append(cmdArgs, string(eventJSON))
	}

	cmd := exec.CommandContext(execCtx, def.Action.Exec, cmdArgs...)
	if def.Action.Dir != "" {
		cmd.Dir = def.Action.Dir
	}

	// Environment variables
	env := os.Environ()
	env = append(env,
		fmt.Sprintf("CLARA_RUN_ID=%s", runID),
		fmt.Sprintf("CLARA_TRIGGER_ID=%s", def.ID),
		fmt.Sprintf("CLARA_TRIGGER_TYPE=%s", def.Type),
	)
	if eventID != "" {
		env = append(env, fmt.Sprintf("CLARA_EVENT_ID=%s", eventID))
	}
	if eventType != "" {
		env = append(env, fmt.Sprintf("CLARA_EVENT_TYPE=%s", eventType))
	}
	if passMode == PassEventEnv && len(eventJSON) > 0 {
		env = append(env, fmt.Sprintf("CLARA_EVENT_JSON=%s", string(eventJSON)))
	}
	for k, v := range def.Action.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = env

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	if passMode == PassEventStdin && len(eventJSON) > 0 {
		cmd.Stdin = bytes.NewReader(eventJSON)
	}

	log.Info().
		Str("run_id", runID).
		Str("trigger", def.ID).
		Str("exec", def.Action.Exec).
		Msg("dispatching external script")

	err := cmd.Run()
	finishedAt := time.Now()
	record.FinishedAt = finishedAt
	record.DurationMs = finishedAt.Sub(startedAt).Milliseconds()
	record.Stdout = stdoutBuf.String()
	record.Stderr = stderrBuf.String()

	if err != nil {
		if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			record.Status = StatusTimeout
			record.Error = fmt.Sprintf("execution timed out after %s", timeout)
		} else {
			record.Status = StatusFailure
			record.Error = err.Error()
		}

		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			record.ExitCode = exitErr.ExitCode()
		} else {
			record.ExitCode = -1
		}

		log.Warn().
			Str("run_id", runID).
			Str("trigger", def.ID).
			Int("exit_code", record.ExitCode).
			Str("error", record.Error).
			Msg("script execution failed (let it fail)")
	} else {
		record.Status = StatusSuccess
		record.ExitCode = 0
		log.Info().
			Str("run_id", runID).
			Str("trigger", def.ID).
			Int64("duration_ms", record.DurationMs).
			Msg("script execution completed successfully")
	}

	return record
}
