package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/cockroachdb/errors"
)

// TriggerRunRecord represents a persisted trigger execution.
type TriggerRunRecord struct {
	ID          string    `json:"id"`
	TriggerID   string    `json:"trigger_id"`
	TriggerType string    `json:"trigger_type"`
	Status      string    `json:"status"`
	ExitCode    int       `json:"exit_code"`
	Stdout      string    `json:"stdout"`
	Stderr      string    `json:"stderr"`
	Error       string    `json:"error"`
	EventData   string    `json:"event_data"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
	DurationMs  int64     `json:"duration_ms"`
}

// ToolCallRecord represents a persisted MCP tool call invocation.
type ToolCallRecord struct {
	ID         string    `json:"id"`
	RunID      string    `json:"run_id"`
	ToolName   string    `json:"tool_name"`
	InputJSON  string    `json:"input_json"`
	OutputJSON string    `json:"output_json"`
	Error      string    `json:"error"`
	DurationMs int64     `json:"duration_ms"`
	CalledAt   time.Time `json:"called_at"`
}

// InitAuditSchema creates audit tables for trigger runs and tool interactions.
func (s *Store) initAuditSchema() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS trigger_runs (
			id           TEXT PRIMARY KEY,
			trigger_id   TEXT NOT NULL,
			trigger_type TEXT NOT NULL,
			status       TEXT NOT NULL,
			exit_code    INTEGER NOT NULL DEFAULT 0,
			stdout       TEXT NOT NULL DEFAULT '',
			stderr       TEXT NOT NULL DEFAULT '',
			error        TEXT NOT NULL DEFAULT '',
			event_data   TEXT NOT NULL DEFAULT '',
			started_at   INTEGER NOT NULL,
			finished_at  INTEGER NOT NULL,
			duration_ms  INTEGER NOT NULL DEFAULT 0
		);

		CREATE INDEX IF NOT EXISTS idx_trigger_runs_trigger_id ON trigger_runs(trigger_id);
		CREATE INDEX IF NOT EXISTS idx_trigger_runs_started_at ON trigger_runs(started_at DESC);

		CREATE TABLE IF NOT EXISTS tool_calls (
			id          TEXT PRIMARY KEY,
			run_id      TEXT NOT NULL DEFAULT '',
			tool_name   TEXT NOT NULL,
			input_json  TEXT NOT NULL DEFAULT '{}',
			output_json TEXT NOT NULL DEFAULT '',
			error       TEXT NOT NULL DEFAULT '',
			duration_ms INTEGER NOT NULL DEFAULT 0,
			called_at   INTEGER NOT NULL
		);

		CREATE INDEX IF NOT EXISTS idx_tool_calls_run_id ON tool_calls(run_id);
		CREATE INDEX IF NOT EXISTS idx_tool_calls_tool_name ON tool_calls(tool_name);
		CREATE INDEX IF NOT EXISTS idx_tool_calls_called_at ON tool_calls(called_at DESC);

		CREATE TABLE IF NOT EXISTS activity_samples (
			id          TEXT PRIMARY KEY,
			app_name    TEXT NOT NULL,
			window_title TEXT NOT NULL DEFAULT '',
			idle_secs   INTEGER NOT NULL DEFAULT 0,
			metadata    TEXT NOT NULL DEFAULT '{}',
			sampled_at  INTEGER NOT NULL
		);

		CREATE INDEX IF NOT EXISTS idx_activity_samples_app_name ON activity_samples(app_name);
		CREATE INDEX IF NOT EXISTS idx_activity_samples_sampled_at ON activity_samples(sampled_at DESC);
	`)
	if err != nil {
		return errors.Wrap(err, "create audit schema")
	}
	return nil
}

// RecordTriggerRun persists or updates a run record.
func (s *Store) RecordTriggerRun(ctx context.Context, r TriggerRunRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO trigger_runs (
			id, trigger_id, trigger_type, status, exit_code,
			stdout, stderr, error, event_data, started_at, finished_at, duration_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			status = excluded.status,
			exit_code = excluded.exit_code,
			stdout = excluded.stdout,
			stderr = excluded.stderr,
			error = excluded.error,
			finished_at = excluded.finished_at,
			duration_ms = excluded.duration_ms
	`,
		r.ID, r.TriggerID, r.TriggerType, r.Status, r.ExitCode,
		r.Stdout, r.Stderr, r.Error, r.EventData,
		r.StartedAt.UnixMilli(), r.FinishedAt.UnixMilli(), r.DurationMs,
	)
	if err != nil {
		return errors.Wrap(err, "record trigger run")
	}
	return nil
}

// GetTriggerRun retrieves a single trigger run by ID.
func (s *Store) GetTriggerRun(ctx context.Context, id string) (*TriggerRunRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, trigger_id, trigger_type, status, exit_code,
		       stdout, stderr, error, event_data, started_at, finished_at, duration_ms
		FROM trigger_runs
		WHERE id = ?
	`, id)

	var r TriggerRunRecord
	var startedAtMs, finishedAtMs int64

	err := row.Scan(
		&r.ID, &r.TriggerID, &r.TriggerType, &r.Status, &r.ExitCode,
		&r.Stdout, &r.Stderr, &r.Error, &r.EventData,
		&startedAtMs, &finishedAtMs, &r.DurationMs,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "get trigger run")
	}

	r.StartedAt = time.UnixMilli(startedAtMs)
	r.FinishedAt = time.UnixMilli(finishedAtMs)
	return &r, nil
}

// ListTriggerRuns returns recent trigger runs.
func (s *Store) ListTriggerRuns(ctx context.Context, limit int, triggerID string) ([]TriggerRunRecord, error) {
	if limit <= 0 {
		limit = 50
	}

	var rows *sql.Rows
	var err error

	if triggerID != "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, trigger_id, trigger_type, status, exit_code,
			       stdout, stderr, error, event_data, started_at, finished_at, duration_ms
			FROM trigger_runs
			WHERE trigger_id = ?
			ORDER BY started_at DESC
			LIMIT ?
		`, triggerID, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, trigger_id, trigger_type, status, exit_code,
			       stdout, stderr, error, event_data, started_at, finished_at, duration_ms
			FROM trigger_runs
			ORDER BY started_at DESC
			LIMIT ?
		`, limit)
	}

	if err != nil {
		return nil, errors.Wrap(err, "list trigger runs")
	}
	defer rows.Close()

	var records []TriggerRunRecord
	for rows.Next() {
		var r TriggerRunRecord
		var startedAtMs, finishedAtMs int64
		if err := rows.Scan(
			&r.ID, &r.TriggerID, &r.TriggerType, &r.Status, &r.ExitCode,
			&r.Stdout, &r.Stderr, &r.Error, &r.EventData,
			&startedAtMs, &finishedAtMs, &r.DurationMs,
		); err != nil {
			return nil, errors.Wrap(err, "scan trigger run")
		}
		r.StartedAt = time.UnixMilli(startedAtMs)
		r.FinishedAt = time.UnixMilli(finishedAtMs)
		records = append(records, r)
	}

	return records, rows.Err()
}

// RecordToolCall records an MCP tool execution.
func (s *Store) RecordToolCall(ctx context.Context, call ToolCallRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tool_calls (
			id, run_id, tool_name, input_json, output_json, error, duration_ms, called_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		call.ID, call.RunID, call.ToolName, call.InputJSON,
		call.OutputJSON, call.Error, call.DurationMs, call.CalledAt.UnixMilli(),
	)
	if err != nil {
		return errors.Wrap(err, "record tool call")
	}
	return nil
}

// ListToolCalls returns recent tool calls.
func (s *Store) ListToolCalls(ctx context.Context, limit int, runID string) ([]ToolCallRecord, error) {
	if limit <= 0 {
		limit = 50
	}

	var rows *sql.Rows
	var err error

	if runID != "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, run_id, tool_name, input_json, output_json, error, duration_ms, called_at
			FROM tool_calls
			WHERE run_id = ?
			ORDER BY called_at DESC
			LIMIT ?
		`, runID, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, run_id, tool_name, input_json, output_json, error, duration_ms, called_at
			FROM tool_calls
			ORDER BY called_at DESC
			LIMIT ?
		`, limit)
	}

	if err != nil {
		return nil, errors.Wrap(err, "list tool calls")
	}
	defer rows.Close()

	var records []ToolCallRecord
	for rows.Next() {
		var c ToolCallRecord
		var calledAtMs int64
		if err := rows.Scan(
			&c.ID, &c.RunID, &c.ToolName, &c.InputJSON,
			&c.OutputJSON, &c.Error, &c.DurationMs, &calledAtMs,
		); err != nil {
			return nil, errors.Wrap(err, "scan tool call")
		}
		c.CalledAt = time.UnixMilli(calledAtMs)
		records = append(records, c)
	}

	return records, rows.Err()
}

// ActivitySampleRecord represents a captured snapshot of active user activity on macOS.
type ActivitySampleRecord struct {
	ID          string    `json:"id"`
	AppName     string    `json:"app_name"`
	WindowTitle string    `json:"window_title"`
	IdleSecs    int64     `json:"idle_secs"`
	Metadata    string    `json:"metadata"`
	SampledAt   time.Time `json:"sampled_at"`
}

// RecordActivitySample persists a desktop activity sample.
func (s *Store) RecordActivitySample(ctx context.Context, sample ActivitySampleRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO activity_samples (id, app_name, window_title, idle_secs, metadata, sampled_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, sample.ID, sample.AppName, sample.WindowTitle, sample.IdleSecs, sample.Metadata, sample.SampledAt.UnixMilli())
	if err != nil {
		return errors.Wrap(err, "record activity sample")
	}
	return nil
}

// ListActivitySamples queries recent activity samples.
func (s *Store) ListActivitySamples(ctx context.Context, limit int, since time.Time) ([]ActivitySampleRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows *sql.Rows
	var err error

	if !since.IsZero() {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, app_name, window_title, idle_secs, metadata, sampled_at
			FROM activity_samples
			WHERE sampled_at >= ?
			ORDER BY sampled_at DESC
			LIMIT ?
		`, since.UnixMilli(), limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, app_name, window_title, idle_secs, metadata, sampled_at
			FROM activity_samples
			ORDER BY sampled_at DESC
			LIMIT ?
		`, limit)
	}
	if err != nil {
		return nil, errors.Wrap(err, "list activity samples")
	}
	defer rows.Close()

	var records []ActivitySampleRecord
	for rows.Next() {
		var a ActivitySampleRecord
		var sampledAtMs int64
		if err := rows.Scan(&a.ID, &a.AppName, &a.WindowTitle, &a.IdleSecs, &a.Metadata, &sampledAtMs); err != nil {
			return nil, errors.Wrap(err, "scan activity sample")
		}
		a.SampledAt = time.UnixMilli(sampledAtMs)
		records = append(records, a)
	}
	return records, rows.Err()
}
