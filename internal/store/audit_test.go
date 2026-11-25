package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/brightpuddle/clara/internal/store"
)

func TestTriggerRun_LinkedFieldsRoundTrip(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Millisecond)

	err := s.RecordTriggerRun(ctx, store.TriggerRunRecord{
		ID: "run-1", TriggerID: "t", TriggerType: "event", Status: "success",
		EventID: "evt-9", ParentRunID: "run-0", SessionID: "ses-3",
		Receipt:   `{"outcome":"ok"}`,
		StartedAt: now, FinishedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.GetTriggerRun(ctx, "run-1")
	if err != nil || got == nil {
		t.Fatalf("get: %v %v", got, err)
	}
	if got.EventID != "evt-9" || got.ParentRunID != "run-0" || got.SessionID != "ses-3" ||
		got.Receipt != `{"outcome":"ok"}` {
		t.Fatalf("linked fields not round-tripped: %+v", got)
	}

	list, err := s.ListTriggerRuns(ctx, 10, "t")
	if err != nil || len(list) != 1 || list[0].EventID != "evt-9" {
		t.Fatalf("list: %+v %v", list, err)
	}
}

func TestTriggerRun_UpsertKeepsLinksWhenLaterWriteOmitsThem(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now()

	first := store.TriggerRunRecord{
		ID: "run-1", TriggerID: "t", TriggerType: "event", Status: "running",
		EventID: "evt-1", StartedAt: now, FinishedAt: now,
	}
	if err := s.RecordTriggerRun(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.Status = "success"
	second.Receipt = `{"outcome":"noop"}`
	if err := s.RecordTriggerRun(ctx, second); err != nil {
		t.Fatal(err)
	}

	got, _ := s.GetTriggerRun(ctx, "run-1")
	if got.Status != "success" || got.Receipt != `{"outcome":"noop"}` || got.EventID != "evt-1" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestListChildRuns(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now()
	for _, r := range []store.TriggerRunRecord{
		{ID: "p", TriggerID: "t", TriggerType: "event", Status: "success", StartedAt: now, FinishedAt: now},
		{ID: "c1", TriggerID: "t2", TriggerType: "manual", Status: "success", ParentRunID: "p", StartedAt: now, FinishedAt: now},
		{ID: "x", TriggerID: "t3", TriggerType: "manual", Status: "success", StartedAt: now, FinishedAt: now},
	} {
		if err := s.RecordTriggerRun(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	kids, err := s.ListChildRuns(ctx, "p")
	if err != nil || len(kids) != 1 || kids[0].ID != "c1" {
		t.Fatalf("kids: %+v %v", kids, err)
	}
}

func TestGetRunTrace(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now()

	for _, r := range []store.TriggerRunRecord{
		{ID: "p", TriggerID: "t", TriggerType: "event", Status: "success", EventID: "e1", StartedAt: now, FinishedAt: now},
		{ID: "c", TriggerID: "t2", TriggerType: "manual", Status: "success", ParentRunID: "p", StartedAt: now, FinishedAt: now},
	} {
		if err := s.RecordTriggerRun(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	for i, name := range []string{"db.query", "mail.move"} {
		if err := s.RecordToolCall(ctx, store.ToolCallRecord{
			ID: name, RunID: "p", ToolName: name, CalledAt: now.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatal(err)
		}
	}

	tr, err := s.GetRunTrace(ctx, "p")
	if err != nil || tr == nil {
		t.Fatalf("trace: %v %v", tr, err)
	}
	if tr.EventID != "e1" || len(tr.Children) != 1 || tr.Children[0].ID != "c" {
		t.Fatalf("children/links: %+v", tr)
	}
	if len(tr.ToolCalls) != 2 || tr.ToolCalls[0].ToolName != "db.query" {
		t.Fatalf("tool calls should be chronological: %+v", tr.ToolCalls)
	}

	if none, err := s.GetRunTrace(ctx, "missing"); err != nil || none != nil {
		t.Fatalf("missing run: %v %v", none, err)
	}
}
