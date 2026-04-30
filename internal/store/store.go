// Package store manages Clara's SQLite database. It initialises a CGO-free
// SQLite instance (via ncruces/go-sqlite3) with the sqlite-vec extension loaded
// and provides the Tool implementations for the interpreter to use.
//
// Tool names registered in the registry:
//
//   - "db.query"      — execute arbitrary read SQL and return rows as JSON
//   - "db.exec"       — execute a write SQL statement (no result rows)
//   - "db.vec_search" — perform a vec0 vector similarity search
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"

	// Side-effect import: loads the ncruces SQLite WASM build with sqlite-vec.
	_ "github.com/asg017/sqlite-vec-go-bindings/ncruces"
	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// init enables the WASM threads feature required by the sqlite-vec binary.
// The sqlite-vec WASM build uses atomic memory operations which need the
// threads proposal enabled in wazero.
func init() {
	features := api.CoreFeaturesV2 | experimental.CoreFeaturesThreads
	sqlite3.RuntimeConfig = wazero.NewRuntimeConfig().WithCoreFeatures(features)
}

type Store struct {
	db  *sql.DB
	log zerolog.Logger
}

type RunState struct {
	RunID        string
	IntentID     string
	State        string
	Status       string
	Error        string
	WorkflowType string
	Entrypoint   string
	ScriptSource string
	WaitName     string
	WaitArgs     any
	StartedAt    int64
	UpdatedAt    int64
	FinishedAt   sql.NullInt64
}

type ReplayHistoryEntry struct {
	Sequence   int
	RunID      string
	IntentID   string
	Entrypoint string
	Kind       string
	Name       string
	Args       any
	Result     any
	Error      string
	CreatedAt  int64
}

type TUIContentItem struct {
	ID          int64
	RunID       string
	IntentID    string
	Kind        string
	Text        string
	Options     []string
	Answer      string
	CreatedAt   int64
}

// Open opens or creates the SQLite database at dbPath.
func Open(dbPath string, log zerolog.Logger) (*Store, error) {
	db, err := driver.Open(dbPath, nil)
	if err != nil {
		return nil, errors.Wrap(err, "open sqlite database")
	}

	s := &Store{db: db, log: log.With().Str("component", "store").Logger()}

	if err := s.migrate(); err != nil {
		db.Close()
		return nil, errors.Wrap(err, "run migrations")
	}
	return s, nil
}

// OpenMemory opens an in-memory SQLite database for testing.
func OpenMemory(log zerolog.Logger) (*Store, error) {
	return Open(":memory:", log)
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying *sql.DB for direct use where needed.
func (s *Store) DB() *sql.DB {
	return s.db
}

// migrate creates the base schema if it does not exist.
// If the legacy blueprint_runs table exists, its data is migrated to intent_runs.
func (s *Store) migrate() error {
	// Create new schema.
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS intent_runs (
			id         TEXT    NOT NULL,
			intent_id  TEXT    NOT NULL,
			state      TEXT    NOT NULL,
			mem_json   TEXT    NOT NULL DEFAULT '{}',
			status     TEXT    NOT NULL DEFAULT 'running',
			error      TEXT    NOT NULL DEFAULT '',
			workflow_type TEXT NOT NULL DEFAULT 'state_machine',
			entrypoint TEXT NOT NULL DEFAULT '',
			script_source TEXT NOT NULL DEFAULT '',
			wait_name TEXT NOT NULL DEFAULT '',
			wait_args_json TEXT NOT NULL DEFAULT 'null',
			started_at INTEGER NOT NULL DEFAULT (unixepoch()),
			updated_at INTEGER NOT NULL DEFAULT (unixepoch()),
			finished_at INTEGER,
			PRIMARY KEY (id)
		);

		CREATE INDEX IF NOT EXISTS idx_intent_runs_intent_id
			ON intent_runs(intent_id);

		CREATE INDEX IF NOT EXISTS idx_intent_runs_status
			ON intent_runs(status);

		CREATE TABLE IF NOT EXISTS intent_events (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id     TEXT    NOT NULL,
			intent_id  TEXT    NOT NULL,
			state      TEXT    NOT NULL,
			action     TEXT    NOT NULL DEFAULT '',
			args_json  TEXT    NOT NULL DEFAULT 'null',
			result_json TEXT   NOT NULL DEFAULT 'null',
			error      TEXT    NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL DEFAULT (unixepoch())
		);

		CREATE INDEX IF NOT EXISTS idx_intent_events_intent_id
			ON intent_events(intent_id);

		CREATE INDEX IF NOT EXISTS idx_intent_events_run_id
			ON intent_events(run_id);

		CREATE TABLE IF NOT EXISTS intent_replay_history (
			run_id      TEXT    NOT NULL,
			sequence    INTEGER NOT NULL,
			intent_id   TEXT    NOT NULL,
			entrypoint  TEXT    NOT NULL DEFAULT '',
			kind        TEXT    NOT NULL,
			name        TEXT    NOT NULL,
			args_json   TEXT    NOT NULL DEFAULT 'null',
			result_json TEXT    NOT NULL DEFAULT 'null',
			error       TEXT    NOT NULL DEFAULT '',
			created_at  INTEGER NOT NULL DEFAULT (unixepoch()),
			PRIMARY KEY (run_id, sequence)
		);

		CREATE INDEX IF NOT EXISTS idx_intent_replay_history_run_id
			ON intent_replay_history(run_id);

		CREATE TABLE IF NOT EXISTS kv_store (
			key        TEXT PRIMARY KEY,
			value_json TEXT NOT NULL,
			updated_at INTEGER NOT NULL DEFAULT (unixepoch())
		);

		CREATE TABLE IF NOT EXISTS tui_command_history (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			command    TEXT    NOT NULL,
			created_at INTEGER NOT NULL DEFAULT (unixepoch())
		);

		CREATE INDEX IF NOT EXISTS idx_tui_command_history_created_at ON tui_command_history(created_at);

		CREATE TABLE IF NOT EXISTS tui_content_history (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id       TEXT    NOT NULL DEFAULT '',
			intent_id    TEXT    NOT NULL DEFAULT '',
			kind         TEXT    NOT NULL, -- 'notification' or 'qa'
			text         TEXT    NOT NULL,
			options_json TEXT    NOT NULL DEFAULT 'null',
			answer       TEXT    NOT NULL DEFAULT '',
			created_at   INTEGER NOT NULL DEFAULT (unixepoch())
		);

		CREATE INDEX IF NOT EXISTS idx_tui_content_history_created_at ON tui_content_history(created_at);
	`)
	if err != nil {
		return errors.Wrap(err, "create schema")
	}

	if err := s.ensureIntentRunsColumns(); err != nil {
		return err
	}

	// Migrate data from the legacy blueprint_runs table if it exists.
	_, _ = s.db.Exec(`
		INSERT OR IGNORE INTO intent_runs (id, intent_id, state, mem_json, started_at, updated_at)
		SELECT id, blueprint_id, state, mem_json, started_at, updated_at FROM blueprint_runs
	`)
	_, _ = s.db.Exec(`DROP TABLE IF EXISTS blueprint_runs`)

	return nil
}

func (s *Store) ensureIntentRunsColumns() error {
	for _, stmt := range []string{
		`ALTER TABLE intent_runs ADD COLUMN status TEXT NOT NULL DEFAULT 'running'`,
		`ALTER TABLE intent_runs ADD COLUMN error TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE intent_runs ADD COLUMN workflow_type TEXT NOT NULL DEFAULT 'state_machine'`,
		`ALTER TABLE intent_runs ADD COLUMN entrypoint TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE intent_runs ADD COLUMN script_source TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE intent_runs ADD COLUMN wait_name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE intent_runs ADD COLUMN wait_args_json TEXT NOT NULL DEFAULT 'null'`,
		`ALTER TABLE intent_runs ADD COLUMN finished_at INTEGER`,
		`ALTER TABLE intent_replay_history ADD COLUMN entrypoint TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := s.db.Exec(stmt); err != nil && !isDuplicateColumnError(err) {
			return errors.Wrap(err, "ensure intent_runs columns")
		}
	}
	return nil
}

func isDuplicateColumnError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate column name")
}

// QueryTool returns a registry Tool that executes a read SQL query.
// Args: sql (string), params ([]any, optional)
func (s *Store) QueryTool() func(ctx context.Context, args map[string]any) (any, error) {
	return func(ctx context.Context, args map[string]any) (any, error) {
		query, ok := args["sql"].(string)
		if !ok || query == "" {
			return nil, errors.New("db.query: 'sql' argument is required")
		}
		params := toSlice(args["params"])

		rows, err := s.db.QueryContext(ctx, query, params...)
		if err != nil {
			return nil, errors.Wrap(err, "db.query")
		}
		defer rows.Close()

		return scanRows(rows)
	}
}

// ExecTool returns a registry Tool that executes a write SQL statement.
// Args: sql (string), params ([]any, optional)
func (s *Store) ExecTool() func(ctx context.Context, args map[string]any) (any, error) {
	return func(ctx context.Context, args map[string]any) (any, error) {
		query, ok := args["sql"].(string)
		if !ok || query == "" {
			return nil, errors.New("db.exec: 'sql' argument is required")
		}
		params := toSlice(args["params"])

		result, err := s.db.ExecContext(ctx, query, params...)
		if err != nil {
			return nil, errors.Wrap(err, "db.exec")
		}
		affected, _ := result.RowsAffected()
		return map[string]any{"rows_affected": affected}, nil
	}
}

// VecSearchTool returns a registry Tool that performs a vector similarity search
// using the sqlite-vec vec0 virtual table.
//
// Required args:
//   - table  (string): vec0 virtual table name
//   - vector ([]float32 or []byte): query vector
//   - limit  (int): max results (default 10)
//
// Optional args:
//   - min_score (float64): minimum similarity score threshold
func (s *Store) VecSearchTool() func(ctx context.Context, args map[string]any) (any, error) {
	return func(ctx context.Context, args map[string]any) (any, error) {
		table, ok := args["table"].(string)
		if !ok || table == "" {
			return nil, errors.New("db.vec_search: 'table' argument is required")
		}

		vectorBytes, err := encodeVector(args["vector"])
		if err != nil {
			return nil, errors.Wrap(err, "db.vec_search: encode vector")
		}

		limit := 10
		if l, ok := args["limit"].(int); ok && l > 0 {
			limit = l
		} else if l, ok := args["limit"].(float64); ok && l > 0 {
			limit = int(l)
		}

		// sqlite-vec knn query: SELECT rowid, distance FROM <table>
		// WHERE <embedding_col> MATCH ? AND k = ?
		// We use a standard pattern that works for vec0 tables.
		query := fmt.Sprintf(
			`SELECT rowid, distance FROM %s WHERE embedding MATCH ? AND k = ?`, table,
		)
		rows, err := s.db.QueryContext(ctx, query, vectorBytes, limit)
		if err != nil {
			return nil, errors.Wrapf(err, "db.vec_search: query table %q", table)
		}
		defer rows.Close()

		type searchResult struct {
			RowID    int64   `json:"rowid"`
			Distance float64 `json:"distance"`
		}

		minScore, _ := args["min_score"].(float64)
		var results []searchResult
		for rows.Next() {
			var r searchResult
			if err := rows.Scan(&r.RowID, &r.Distance); err != nil {
				return nil, errors.Wrap(err, "db.vec_search: scan row")
			}
			// sqlite-vec returns distance (lower = more similar). A min_score
			// here acts as a max-distance filter when provided.
			if minScore > 0 && r.Distance > minScore {
				continue
			}
			results = append(results, r)
		}
		if err := rows.Err(); err != nil {
			return nil, errors.Wrap(err, "db.vec_search: rows error")
		}
		return results, nil
	}
}

// SaveRunState persists the current execution state of an intent run to
// survive daemon restarts.
func (s *Store) SaveRunState(
	ctx context.Context,
	runID, intentID, state string,
	mem map[string]any,
) error {
	memJSON, err := json.Marshal(mem)
	if err != nil {
		return errors.Wrap(err, "marshal mem")
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO intent_runs (
			id, intent_id, state, mem_json, status, error, workflow_type, entrypoint, script_source,
			wait_name, wait_args_json, updated_at, finished_at
		)
		VALUES (?, ?, ?, ?, 'running', '', 'state_machine', '', '', '', 'null', unixepoch(), NULL)
		ON CONFLICT(id) DO UPDATE SET
			state      = excluded.state,
			mem_json   = excluded.mem_json,
			intent_id  = excluded.intent_id,
			status     = 'running',
			error      = '',
			wait_name  = '',
			wait_args_json = 'null',
			finished_at = NULL,
			updated_at = unixepoch()
	`, runID, intentID, state, string(memJSON))
	return errors.Wrap(err, "save run state")
}

func (s *Store) InitRun(
	ctx context.Context,
	runID, intentID, state, workflowType, entrypoint, scriptSource string,
	mem map[string]any,
) error {
	memJSON, err := json.Marshal(memOrEmpty(mem))
	if err != nil {
		return errors.Wrap(err, "marshal run mem")
	}
	if workflowType == "" {
		workflowType = "state_machine"
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO intent_runs (
			id, intent_id, state, mem_json, status, error, workflow_type, entrypoint, script_source,
			wait_name, wait_args_json, updated_at, finished_at
		)
		VALUES (?, ?, ?, ?, 'running', '', ?, ?, ?, '', 'null', unixepoch(), NULL)
		ON CONFLICT(id) DO UPDATE SET
			intent_id = excluded.intent_id,
			state = excluded.state,
			mem_json = excluded.mem_json,
			status = 'running',
			error = '',
			workflow_type = excluded.workflow_type,
			entrypoint = excluded.entrypoint,
			script_source = excluded.script_source,
			wait_name = '',
			wait_args_json = 'null',
			finished_at = NULL,
			updated_at = unixepoch()
	`, runID, intentID, state, string(memJSON), workflowType, entrypoint, scriptSource)
	return errors.Wrap(err, "init run")
}

func (s *Store) FinishRun(ctx context.Context, runID, status, errorText string) error {
	if status == "" {
		status = "completed"
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE intent_runs
		SET status = ?, error = ?, wait_name = '', wait_args_json = 'null',
		    finished_at = unixepoch(), updated_at = unixepoch()
		WHERE id = ?
	`, status, errorText, runID)
	return errors.Wrap(err, "finish run")
}

func (s *Store) MarkRunWaiting(ctx context.Context, runID, waitName string, waitArgs any) error {
	waitArgsJSON, err := jsonValue(waitArgs)
	if err != nil {
		return errors.Wrap(err, "marshal wait args")
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE intent_runs
		SET status = 'waiting', wait_name = ?, wait_args_json = ?, updated_at = unixepoch()
		WHERE id = ?
	`, waitName, waitArgsJSON, runID)
	return errors.Wrap(err, "mark run waiting")
}

// LoadRunState loads a previously-saved run state for resumption after restart.
// Returns ("", nil, nil) if the run does not exist.
func (s *Store) LoadRunState(
	ctx context.Context,
	runID string,
) (state string, mem map[string]any, err error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT state, mem_json FROM intent_runs WHERE id = ?`, runID,
	)
	var memJSON string
	if err = row.Scan(&state, &memJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil, nil
		}
		return "", nil, errors.Wrap(err, "load run state")
	}
	if err = json.Unmarshal([]byte(memJSON), &mem); err != nil {
		return "", nil, errors.Wrap(err, "unmarshal mem json")
	}
	return state, mem, nil
}

func (s *Store) ActiveRunStates(ctx context.Context, intentID string) ([]RunState, error) {
	query := `
		SELECT id, intent_id, state, status, error, started_at, updated_at, finished_at
		     , workflow_type, entrypoint, script_source, wait_name, wait_args_json
		FROM intent_runs
		WHERE status IN ('running', 'waiting')
	`
	args := []any{}
	if intentID != "" {
		query += ` AND intent_id = ?`
		args = append(args, intentID)
	}
	query += ` ORDER BY intent_id, started_at`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "query active run states")
	}
	defer rows.Close()

	states := []RunState{}
	for rows.Next() {
		var (
			state        RunState
			waitArgsJSON string
		)
		if err := rows.Scan(
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
		); err != nil {
			return nil, errors.Wrap(err, "scan active run state")
		}
		if err := json.Unmarshal([]byte(waitArgsJSON), &state.WaitArgs); err != nil {
			return nil, errors.Wrap(err, "decode active wait args")
		}
		states = append(states, state)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "active run states rows error")
	}
	return states, nil
}

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

func (s *Store) LoadRun(ctx context.Context, runID string) (RunState, map[string]any, error) {
	var (
		state        RunState
		waitArgsJSON string
		memJSON      string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT id, intent_id, state, status, error, workflow_type, entrypoint, script_source,
		       wait_name, wait_args_json, mem_json, started_at, updated_at, finished_at
		FROM intent_runs
		WHERE id = ?
	`, runID).Scan(
		&state.RunID,
		&state.IntentID,
		&state.State,
		&state.Status,
		&state.Error,
		&state.WorkflowType,
		&state.Entrypoint,
		&state.ScriptSource,
		&state.WaitName,
		&waitArgsJSON,
		&memJSON,
		&state.StartedAt,
		&state.UpdatedAt,
		&state.FinishedAt,
	)
	if err != nil {
		return RunState{}, nil, errors.Wrap(err, "load run")
	}
	mem := map[string]any{}
	if err := json.Unmarshal([]byte(memJSON), &mem); err != nil {
		return RunState{}, nil, errors.Wrap(err, "decode run mem")
	}
	if err := json.Unmarshal([]byte(waitArgsJSON), &state.WaitArgs); err != nil {
		return RunState{}, nil, errors.Wrap(err, "decode wait args")
	}
	return state, mem, nil
}

func (s *Store) LoadLatestWaitingRun(
	ctx context.Context,
	intentID string,
) (RunState, map[string]any, error) {
	var runID string
	err := s.db.QueryRowContext(ctx, `
		SELECT id
		FROM intent_runs
		WHERE intent_id = ? AND status = 'waiting'
		ORDER BY updated_at DESC
		LIMIT 1
	`, intentID).Scan(&runID)
	if err != nil {
		return RunState{}, nil, errors.Wrap(err, "load latest waiting run id")
	}
	return s.LoadRun(ctx, runID)
}

func (s *Store) AppendReplayHistory(ctx context.Context, entry ReplayHistoryEntry) error {
	argsJSON, err := jsonValue(entry.Args)
	if err != nil {
		return errors.Wrap(err, "marshal history args")
	}
	resultJSON, err := jsonValue(entry.Result)
	if err != nil {
		return errors.Wrap(err, "marshal history result")
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO intent_replay_history (
			run_id, sequence, intent_id, entrypoint, kind, name, args_json, result_json, error, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, unixepoch())
	`, entry.RunID, entry.Sequence, entry.IntentID, entry.Entrypoint, entry.Kind, entry.Name, argsJSON, resultJSON, entry.Error)
	return errors.Wrap(err, "append replay history")
}

func (s *Store) LoadReplayHistory(ctx context.Context, runID string) ([]ReplayHistoryEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT sequence, run_id, intent_id, entrypoint, kind, name, args_json, result_json, error, created_at
		FROM intent_replay_history
		WHERE run_id = ?
		ORDER BY sequence
	`, runID)
	if err != nil {
		return nil, errors.Wrap(err, "query replay history")
	}
	defer rows.Close()

	history := make([]ReplayHistoryEntry, 0)
	for rows.Next() {
		var (
			entry      ReplayHistoryEntry
			argsJSON   string
			resultJSON string
		)
		if err := rows.Scan(
			&entry.Sequence,
			&entry.RunID,
			&entry.IntentID,
			&entry.Entrypoint,
			&entry.Kind,
			&entry.Name,
			&argsJSON,
			&resultJSON,
			&entry.Error,
			&entry.CreatedAt,
		); err != nil {
			return nil, errors.Wrap(err, "scan replay history")
		}
		if err := json.Unmarshal([]byte(argsJSON), &entry.Args); err != nil {
			return nil, errors.Wrap(err, "decode replay args")
		}
		if err := json.Unmarshal([]byte(resultJSON), &entry.Result); err != nil {
			return nil, errors.Wrap(err, "decode replay result")
		}
		history = append(history, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "replay history rows error")
	}
	return history, nil
}



func scanRows(rows *sql.Rows) ([]map[string]any, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, errors.Wrap(err, "get columns")
	}

	var results []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, errors.Wrap(err, "scan row")
		}
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			row[col] = vals[i]
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows error")
	}
	return results, nil
}

// toSlice converts an args value to []any for use as SQL parameters.
func toSlice(v any) []any {
	if v == nil {
		return nil
	}
	if s, ok := v.([]any); ok {
		return s
	}
	return []any{v}
}

func jsonValue(v any) (string, error) {
	if v == nil {
		return "null", nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func memOrEmpty(mem map[string]any) map[string]any {
	if mem == nil {
		return map[string]any{}
	}
	return mem
}

// encodeVector converts a vector argument ([]float32 or []byte) to bytes
// suitable for sqlite-vec MATCH queries.
func encodeVector(v any) ([]byte, error) {
	switch val := v.(type) {
	case []byte:
		return val, nil
	case []float32:
		// Encode as little-endian IEEE 754 floats (sqlite-vec format).
		buf := make([]byte, len(val)*4)
		for i, f := range val {
			bits := math.Float32bits(f)
			buf[i*4] = byte(bits)
			buf[i*4+1] = byte(bits >> 8)
			buf[i*4+2] = byte(bits >> 16)
			buf[i*4+3] = byte(bits >> 24)
		}
		return buf, nil
	case nil:
		return nil, errors.New("vector is required")
	default:
		return nil, errors.Newf("unsupported vector type %T", v)
	}
}

// SaveTUIContent persists a new notification or interactive Q&A to the database.
func (s *Store) SaveTUIContent(ctx context.Context, item TUIContentItem) (int64, error) {
	optionsJSON := "null"
	if len(item.Options) > 0 {
		raw, err := json.Marshal(item.Options)
		if err == nil {
			optionsJSON = string(raw)
		}
	}

	res, err := s.db.ExecContext(ctx, `
		INSERT INTO tui_content_history (run_id, intent_id, kind, text, options_json, answer)
		VALUES (?, ?, ?, ?, ?, ?)
	`, item.RunID, item.IntentID, item.Kind, item.Text, optionsJSON, item.Answer)
	if err != nil {
		return 0, errors.Wrap(err, "save tui content")
	}

	id, _ := res.LastInsertId()
	return id, nil
}

// LoadTUIContentHistory retrieves the latest items from tui_content_history.
func (s *Store) LoadTUIContentHistory(ctx context.Context, limit int) ([]TUIContentItem, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, run_id, intent_id, kind, text, options_json, answer, created_at
		FROM tui_content_history
		ORDER BY created_at ASC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, errors.Wrap(err, "load tui content history")
	}
	defer rows.Close()

	var result []TUIContentItem
	for rows.Next() {
		var item TUIContentItem
		var optionsJSON string
		if err := rows.Scan(
			&item.ID, &item.RunID, &item.IntentID, &item.Kind, &item.Text, &optionsJSON, &item.Answer, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		if optionsJSON != "null" && optionsJSON != "" {
			_ = json.Unmarshal([]byte(optionsJSON), &item.Options)
		}
		result = append(result, item)
	}
	return result, nil
}
// UpdateTUIContentAnswer records the user's response to an interactive prompt.
func (s *Store) UpdateTUIContentAnswer(ctx context.Context, id int64, answer string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE tui_content_history SET answer = ? WHERE id = ?
	`, answer, id)
	return errors.Wrap(err, "update tui content answer")
}

// GetTUIAnswer retrieves the most recent answer for a given intent and prompt text.
func (s *Store) GetTUIAnswer(ctx context.Context, intentID, text string) (string, error) {
	var answer string
	err := s.db.QueryRowContext(ctx, `
		SELECT answer
		FROM tui_content_history
		WHERE intent_id = ? AND text = ? AND answer != ''
		ORDER BY created_at DESC
		LIMIT 1
	`, intentID, text).Scan(&answer)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", errors.Wrap(err, "get tui answer")
	}
	return answer, nil
}

// GetUnansweredTUIPrompt returns the ID of the most recent unanswered prompt for an intent.
func (s *Store) GetUnansweredTUIPrompt(ctx context.Context, intentID, text string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id
		FROM tui_content_history
		WHERE intent_id = ? AND text = ? AND answer = ''
		ORDER BY created_at DESC
		LIMIT 1
	`, intentID, text).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, errors.Wrap(err, "get unanswered tui prompt")
	}
	return id, nil
}

// DeleteTUIContent removes a single item from history.
func (s *Store) DeleteTUIContent(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM tui_content_history WHERE id = ?", id)
	return errors.Wrap(err, "delete tui content")
}

// ClearTUIContentHistory removes all content from history.
func (s *Store) ClearTUIContentHistory(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM tui_content_history")
	return errors.Wrap(err, "clear tui content history")
}

// SaveTUICommand persists a command to the TUI command history.
func (s *Store) SaveTUICommand(ctx context.Context, command string) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO tui_command_history (command) VALUES (?)", command)
	if err != nil {
		return errors.Wrap(err, "save tui command")
	}
	// Prune to keep only latest 100
	_, _ = s.db.ExecContext(ctx, `
		DELETE FROM tui_command_history 
		WHERE id NOT IN (SELECT id FROM tui_command_history ORDER BY created_at DESC LIMIT 100)
	`)
	return nil
}

// LoadTUICommandHistory retrieves all stored TUI commands.
func (s *Store) LoadTUICommandHistory(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT command FROM tui_command_history ORDER BY created_at ASC")
	if err != nil {
		return nil, errors.Wrap(err, "load tui command history")
	}
	defer rows.Close()

	var result []string
	for rows.Next() {
		var cmd string
		if err := rows.Scan(&cmd); err != nil {
			return nil, err
		}
		result = append(result, cmd)
	}
	return result, nil
}
