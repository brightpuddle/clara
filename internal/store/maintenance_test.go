package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ncruces/go-sqlite3/driver"
	"github.com/rs/zerolog"
)

func openFile(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "clara.db")
	s, err := Open(path, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

func pragma(t *testing.T, db *sql.DB, name string) string {
	t.Helper()
	var v string
	if err := db.QueryRow("PRAGMA " + name).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestOpen_UsesWALAndIncrementalVacuum(t *testing.T) {
	s, _ := openFile(t)
	if got := pragma(t, s.DB(), "journal_mode"); got != "wal" {
		t.Errorf("journal_mode = %q, want wal", got)
	}
	if got := pragma(t, s.DB(), "auto_vacuum"); got != "2" {
		t.Errorf("auto_vacuum = %q, want 2 (incremental)", got)
	}
}

// With WAL, a reader must not block behind an open write transaction.
func TestOpen_ReadersDoNotBlockOnWriter(t *testing.T) {
	s, path := openFile(t)
	ctx := context.Background()
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO kv_store(key, value_json) VALUES ('a','1')`); err != nil {
		t.Fatal(err)
	}
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, `INSERT INTO kv_store(key, value_json) VALUES ('b','2')`); err != nil {
		t.Fatal(err)
	}

	r, err := driver.Open(sqliteDSN(path), nil)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer r.Close()
	done := make(chan error, 1)
	go func() {
		var n int
		done <- r.QueryRow(`SELECT count(*) FROM kv_store`).Scan(&n)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("read during write: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reader blocked behind writer")
	}
}

func seedOldAndNew(t *testing.T, s *Store, old, recent time.Time) {
	t.Helper()
	ctx := context.Background()
	for i, ts := range []time.Time{old, old, recent} {
		err := s.RecordToolCall(ctx, ToolCallRecord{
			ID: string(rune('a' + i)), ToolName: "t", CalledAt: ts,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	exec := func(q string, args ...any) {
		if _, err := s.DB().ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO intent_events (run_id, intent_id, state, created_at) VALUES ('r','i','s',?),('r','i','s',?)`,
		old.Unix(), recent.Unix())
	exec(`INSERT INTO intent_replay_history (run_id, sequence, intent_id, kind, name, created_at)
	      VALUES ('r',1,'i','k','n',?),('r',2,'i','k','n',?)`, old.Unix(), recent.Unix())
	exec(`INSERT INTO intent_runs (id, intent_id, state, status, updated_at) VALUES
	      ('old','i','s','completed',?),('new','i','s','completed',?),('live','i','s','running',?)`,
		old.Unix(), recent.Unix(), old.Unix())
	exec(`INSERT INTO trigger_runs (id, trigger_id, trigger_type, status, started_at, finished_at)
	      VALUES ('t1','x','event','ok',?,?),('t2','x','event','ok',?,?)`,
		old.UnixMilli(), old.UnixMilli(), recent.UnixMilli(), recent.UnixMilli())
}

func count(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	if err := s.DB().QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPrune_RemovesOldRowsAndKeepsRecentAndLive(t *testing.T) {
	s, _ := openFile(t)
	now := time.Now()
	seedOldAndNew(t, s, now.Add(-90*24*time.Hour), now.Add(-time.Hour))

	deleted, err := s.Prune(context.Background(), now.Add(-30*24*time.Hour), 1)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2+1+1+1+1 { // tool_calls, events, replay, runs(old only), trigger_runs
		t.Errorf("deleted = %d, want 6", deleted)
	}
	for table, want := range map[string]int{
		"tool_calls": 1, "intent_events": 1, "intent_replay_history": 1,
		"intent_runs":  2, // recent run and the still-running one survive
		"trigger_runs": 1,
	} {
		if got := count(t, s, table); got != want {
			t.Errorf("%s has %d rows, want %d", table, got, want)
		}
	}
}

func TestPrune_BatchesUntilDone(t *testing.T) {
	s, _ := openFile(t)
	ctx := context.Background()
	old := time.Now().Add(-90 * 24 * time.Hour)
	for i := 0; i < 25; i++ {
		if err := s.RecordToolCall(ctx, ToolCallRecord{ID: string(rune('A' + i)), ToolName: "t", CalledAt: old}); err != nil {
			t.Fatal(err)
		}
	}
	deleted, err := s.Prune(ctx, time.Now().Add(-30*24*time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 25 || count(t, s, "tool_calls") != 0 {
		t.Errorf("deleted %d, remaining %d; want all 25 removed", deleted, count(t, s, "tool_calls"))
	}
}

func TestPrune_ShrinksTheFile(t *testing.T) {
	s, path := openFile(t)
	ctx := context.Background()
	old := time.Now().Add(-90 * 24 * time.Hour)
	blob := string(make([]byte, 20_000))
	for i := 0; i < 400; i++ {
		err := s.RecordToolCall(ctx, ToolCallRecord{
			ID: fmt.Sprint(i), ToolName: "t", OutputJSON: blob, CalledAt: old,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	size := func() int64 {
		// Fold the WAL back so the main file reflects the true size.
		_, _ = s.DB().ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		return st.Size()
	}
	before := size()
	if _, err := s.Prune(ctx, time.Now().Add(-30*24*time.Hour), 100); err != nil {
		t.Fatal(err)
	}
	if after := size(); after > before/4 {
		t.Errorf("file is %d bytes after prune, was %d; want a large reduction", after, before)
	}
}
