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
