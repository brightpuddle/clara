package intentlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendAndReadAll(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	now := time.Now().Truncate(time.Second)
	events := []Event{
		{
			Time:       now,
			RunID:      "r1",
			IntentID:   "hello",
			Entrypoint: "main",
			State:      "INIT",
			Action:     "start",
		},
		{
			Time:       now.Add(time.Second),
			RunID:      "r1",
			IntentID:   "hello",
			Entrypoint: "main",
			State:      "RUN",
			Action:     "db.query",
			Args:       map[string]any{"sql": "select 1"},
		},
		{
			Time:     now.Add(2 * time.Second),
			RunID:    "r1",
			IntentID: "hello",
			State:    "DONE",
			Action:   "finish",
			Result:   map[string]any{"status": "completed"},
		},
	}
	for _, e := range events {
		if err := l.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	got, err := ReadEvents(l.FilePath("hello"), Filter{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 events, got %d", len(got))
	}
	if got[1].Action != "db.query" {
		t.Fatalf("unexpected action: %q", got[1].Action)
	}
}

func TestReadTail(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	now := time.Now().Truncate(time.Second)
	for i := range 10 {
		if err := l.Append(Event{
			Time:     now.Add(time.Duration(i) * time.Second),
			RunID:    "r1",
			IntentID: "hello",
			State:    "RUN",
			Action:   "step",
		}); err != nil {
			t.Fatal(err)
		}
	}

	got, err := ReadEvents(l.FilePath("hello"), Filter{}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 tail events, got %d", len(got))
	}
	// Should be the last 3, in chronological order.
	if got[0].Time != now.Add(7*time.Second) {
		t.Fatalf("unexpected first tail event time: %v", got[0].Time)
	}
	if got[2].Time != now.Add(9*time.Second) {
		t.Fatalf("unexpected last tail event time: %v", got[2].Time)
	}
}

func TestFilterByEntrypoint(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	now := time.Now().Truncate(time.Second)
	_ = l.Append(Event{Time: now, RunID: "r1", IntentID: "hello", Entrypoint: "main", State: "RUN"})
	_ = l.Append(
		Event{
			Time:       now.Add(time.Second),
			RunID:      "r2",
			IntentID:   "hello",
			Entrypoint: "other",
			State:      "RUN",
		},
	)
	_ = l.Append(
		Event{
			Time:       now.Add(2 * time.Second),
			RunID:      "r1",
			IntentID:   "hello",
			Entrypoint: "main",
			State:      "DONE",
		},
	)

	got, err := ReadEvents(l.FilePath("hello"), Filter{Entrypoint: "main"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 events for entrypoint=main, got %d", len(got))
	}
}

func TestFilterBySince(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	base := time.Now().Truncate(time.Second)
	for i := range 5 {
		_ = l.Append(
			Event{
				Time:     base.Add(time.Duration(i) * time.Second),
				RunID:    "r1",
				IntentID: "hello",
				State:    "RUN",
			},
		)
	}

	// Only events strictly after base+2s.
	got, err := ReadEvents(l.FilePath("hello"), Filter{Since: base.Add(2 * time.Second)}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 events after since filter, got %d", len(got))
	}
}

func TestMergeEvents(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	base := time.Now().Truncate(time.Second)
	_ = l.Append(Event{Time: base, RunID: "r1", IntentID: "alpha", State: "RUN"})
	_ = l.Append(
		Event{Time: base.Add(2 * time.Second), RunID: "r2", IntentID: "beta", State: "RUN"},
	)
	_ = l.Append(Event{Time: base.Add(time.Second), RunID: "r1", IntentID: "alpha", State: "DONE"})
	l.Close()

	got, err := MergeEvents(dir, Filter{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 merged events, got %d", len(got))
	}
	// Verify chronological order after merge.
	if got[0].IntentID != "alpha" || got[1].IntentID != "alpha" || got[2].IntentID != "beta" {
		t.Fatalf("unexpected merge order: %v", got)
	}
}

func TestMergeTail(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	base := time.Now().Truncate(time.Second)
	for i := range 5 {
		_ = l.Append(
			Event{Time: base.Add(time.Duration(i) * time.Second), RunID: "r1", IntentID: "alpha"},
		)
		_ = l.Append(
			Event{
				Time:     base.Add(time.Duration(i)*time.Second + 500*time.Millisecond),
				RunID:    "r2",
				IntentID: "beta",
			},
		)
	}
	l.Close()

	got, err := MergeEvents(dir, Filter{}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 merged tail events, got %d", len(got))
	}
}

func TestClearEvents(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	_ = l.Append(Event{Time: time.Now(), RunID: "r1", IntentID: "hello"})
	_ = l.Append(Event{Time: time.Now(), RunID: "r2", IntentID: "world"})
	l.Close()

	if err := ClearEvents(dir, "hello"); err != nil {
		t.Fatal(err)
	}

	// hello.log should be empty (truncated).
	info, err := os.Stat(filepath.Join(dir, "hello.log"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatalf("expected hello.log to be truncated, size=%d", info.Size())
	}

	// world.log should be untouched.
	info, err = os.Stat(filepath.Join(dir, "world.log"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatal("expected world.log to still have content")
	}
}

func TestClearAllEvents(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	_ = l.Append(Event{Time: time.Now(), RunID: "r1", IntentID: "alpha"})
	_ = l.Append(Event{Time: time.Now(), RunID: "r2", IntentID: "beta"})
	l.Close()

	if err := ClearEvents(dir, ""); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"alpha.log", "beta.log"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() != 0 {
			t.Fatalf("expected %s to be truncated", name)
		}
	}
}

func TestEventJSONRoundtrip(t *testing.T) {
	e := Event{
		Time:       time.Now().UTC().Truncate(time.Millisecond),
		RunID:      "r1",
		IntentID:   "hello",
		Entrypoint: "main",
		State:      "RUN",
		Action:     "db.query",
		Args:       map[string]any{"sql": "select 1"},
		Result:     map[string]any{"rows": []any{1.0}},
		Error:      "",
	}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"run_id":"r1"`) {
		t.Fatalf("unexpected JSON: %s", data)
	}
	var back Event
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.RunID != e.RunID || back.Action != e.Action {
		t.Fatalf("roundtrip mismatch: %+v", back)
	}
}

func TestReadEventsNonexistentFile(t *testing.T) {
	events, err := ReadEvents("/tmp/does-not-exist-intentlog.log", Filter{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("expected empty slice for missing file, got %d events", len(events))
	}
}
