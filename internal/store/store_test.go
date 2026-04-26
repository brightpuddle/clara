package store_test

import (
	"context"
	"testing"

	"github.com/brightpuddle/clara/internal/store"
	"github.com/rs/zerolog"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.OpenMemory(zerolog.Nop())
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestStore_QueryTool_BasicSelect(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// Create a test table.
	_, err := s.DB().ExecContext(ctx, `CREATE TABLE t (id INTEGER, name TEXT)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB().ExecContext(ctx, `INSERT INTO t VALUES (1, 'alice'), (2, 'bob')`)
	if err != nil {
		t.Fatal(err)
	}

	tool := s.QueryTool()
	result, err := tool(ctx, map[string]any{"sql": "SELECT * FROM t ORDER BY id"})
	if err != nil {
		t.Fatalf("QueryTool: %v", err)
	}

	rows, ok := result.([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]any, got %T", result)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
}

func TestStore_QueryTool_MissingSQL(t *testing.T) {
	s := openTestStore(t)
	tool := s.QueryTool()
	_, err := tool(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing sql arg")
	}
}

func TestStore_ExecTool(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	s.DB().ExecContext(ctx, `CREATE TABLE e (x INTEGER)`) //nolint:errcheck

	tool := s.ExecTool()
	result, err := tool(ctx, map[string]any{
		"sql":    "INSERT INTO e VALUES (?)",
		"params": []any{42},
	})
	if err != nil {
		t.Fatalf("ExecTool: %v", err)
	}

	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if m["rows_affected"] != int64(1) {
		t.Errorf("rows_affected: got %v want 1", m["rows_affected"])
	}
}

func TestStore_SaveAndLoadRunState(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	mem := map[string]any{"count": 3, "status": "ok"}
	if err := s.SaveRunState(ctx, "run-1", "intent-1", "RECONCILE", mem); err != nil {
		t.Fatalf("SaveRunState: %v", err)
	}

	state, loadedMem, err := s.LoadRunState(ctx, "run-1")
	if err != nil {
		t.Fatalf("LoadRunState: %v", err)
	}
	if state != "RECONCILE" {
		t.Errorf("state: got %q want %q", state, "RECONCILE")
	}
	if loadedMem["status"] != "ok" {
		t.Errorf("mem.status: got %v want %q", loadedMem["status"], "ok")
	}
}

func TestStore_LoadRunState_NotFound(t *testing.T) {
	s := openTestStore(t)
	state, mem, err := s.LoadRunState(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != "" || mem != nil {
		t.Errorf("expected empty result for missing run, got state=%q mem=%v", state, mem)
	}
}

func TestStore_SaveRunState_Upsert(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	s.SaveRunState(ctx, "run-2", "intent-1", "START", nil)                        //nolint:errcheck
	s.SaveRunState(ctx, "run-2", "intent-1", "END", map[string]any{"done": true}) //nolint:errcheck

	state, mem, err := s.LoadRunState(ctx, "run-2")
	if err != nil {
		t.Fatal(err)
	}
	if state != "END" {
		t.Errorf("state: got %q want END", state)
	}
	if mem["done"] != true {
		t.Errorf("mem.done: got %v want true", mem["done"])
	}
}

func TestStore_RunEventsAndActiveStates(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if err := s.SaveRunState(ctx, "run-3", "intent-3", "WAIT", map[string]any{"waiting": true}); err != nil {
		t.Fatalf("SaveRunState: %v", err)
	}
	if err := s.AppendRunEvent(ctx, store.RunEvent{
		RunID:    "run-3",
		IntentID: "intent-3",
		State:    "WAIT",
		Action:   "prompt.user",
		Args:     map[string]any{"message": "hello"},
		Result:   map[string]any{"waiting": true},
	}); err != nil {
		t.Fatalf("AppendRunEvent: %v", err)
	}

	states, err := s.ActiveRunStates(ctx, "intent-3")
	if err != nil {
		t.Fatalf("ActiveRunStates: %v", err)
	}
	if len(states) != 1 || states[0].State != "WAIT" || states[0].Status != "running" {
		t.Fatalf("unexpected active states: %#v", states)
	}

	events, err := s.RunEventsSince(ctx, 0, "intent-3")
	if err != nil {
		t.Fatalf("RunEventsSince: %v", err)
	}
	if len(events) != 1 || events[0].Action != "prompt.user" {
		t.Fatalf("unexpected events: %#v", events)
	}

	if err := s.FinishRun(ctx, "run-3", "completed", ""); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}
	events, err = s.RunEventsSince(ctx, 0, "intent-3")
	if err != nil {
		t.Fatalf("RunEventsSince after finish: %v", err)
	}
	if len(events) != 2 || events[1].Result == nil {
		t.Fatalf("unexpected finish events: %#v", events)
	}
	states, err = s.ActiveRunStates(ctx, "intent-3")
	if err != nil {
		t.Fatalf("ActiveRunStates after finish: %v", err)
	}
	if len(states) != 0 {
		t.Fatalf("expected no active states after finish, got %#v", states)
	}
}

func TestStore_ReplayHistoryAndWaitingRun(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if err := s.InitRun(ctx, "run-script", "intent-script", "SCRIPT", "native", "", `/path/to/plugin`, nil); err != nil {
		t.Fatalf("InitRun: %v", err)
	}
	if err := s.AppendReplayHistory(ctx, store.ReplayHistoryEntry{
		RunID:    "run-script",
		IntentID: "intent-script",
		Sequence: 0,
		Kind:     "tool",
		Name:     "echo",
		Args:     map[string]any{"value": "ok"},
		Result:   map[string]any{"value": "ok"},
	}); err != nil {
		t.Fatalf("AppendReplayHistory: %v", err)
	}
	if err := s.MarkRunWaiting(ctx, "run-script", "approval", map[string]any{"prompt": "Continue?"}); err != nil {
		t.Fatalf("MarkRunWaiting: %v", err)
	}

	history, err := s.LoadReplayHistory(ctx, "run-script")
	if err != nil {
		t.Fatalf("LoadReplayHistory: %v", err)
	}
	if len(history) != 1 || history[0].Name != "echo" {
		t.Fatalf("unexpected replay history: %#v", history)
	}

	state, mem, err := s.LoadLatestWaitingRun(ctx, "intent-script")
	if err != nil {
		t.Fatalf("LoadLatestWaitingRun: %v", err)
	}
	if state.Status != "waiting" || state.WaitName != "approval" ||
		state.WorkflowType != "native" {
		t.Fatalf("unexpected waiting state: %#v", state)
	}
	if len(mem) != 0 {
		t.Fatalf("expected empty mem, got %#v", mem)
	}
}

func TestStore_TUIContentHistory(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	item := store.TUIContentItem{
		RunID:    "run-1",
		IntentID: "intent-1",
		Kind:     "qa",
		Text:     "Ready?",
		Options:  []string{"Yes", "No"},
	}

	id, err := s.SaveTUIContent(ctx, item)
	if err != nil {
		t.Fatalf("SaveTUIContent: %v", err)
	}

	// Verify it can be loaded
	history, err := s.LoadTUIContentHistory(ctx, 10)
	if err != nil {
		t.Fatalf("LoadTUIContentHistory: %v", err)
	}
	if len(history) != 1 || history[0].Text != "Ready?" {
		t.Fatalf("unexpected history: %#v", history)
	}

	// Answer it
	if err := s.UpdateTUIContentAnswer(ctx, id, "Yes"); err != nil {
		t.Fatalf("UpdateTUIContentAnswer: %v", err)
	}

	// Verify GetTUIAnswer works
	answer, err := s.GetTUIAnswer(ctx, "intent-1", "Ready?")
	if err != nil {
		t.Fatalf("GetTUIAnswer: %v", err)
	}
	if answer != "Yes" {
		t.Errorf("expected answer Yes, got %q", answer)
	}

	// Verify it returns empty for unknown or unanswered
	answer, _ = s.GetTUIAnswer(ctx, "intent-1", "Other?")
	if answer != "" {
		t.Errorf("expected empty answer for unknown prompt")
	}
}
