package report_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/brightpuddle/clara/internal/report"
	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/trigger"
)

var now = time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

type src []store.TriggerRunRecord

func (s src) ListRunSummariesSince(_ context.Context, since time.Time) ([]store.TriggerRunRecord, error) {
	var out []store.TriggerRunRecord
	for _, r := range s {
		if !r.StartedAt.Before(since) {
			out = append(out, r)
		}
	}
	return out, nil
}

func run(id string, ago time.Duration, status, receipt string) store.TriggerRunRecord {
	return store.TriggerRunRecord{ID: fmt.Sprintf("%s-%d", id, ago), TriggerID: id, Status: status,
		StartedAt: now.Add(-ago), Receipt: receipt, DurationMs: 1000}
}

func defs() []trigger.Definition {
	return []trigger.Definition{
		{ID: "mail", Name: "Mail", Enabled: true, Type: trigger.TypeSchedule, Expect: &trigger.Expect{MaxSilence: time.Hour}},
		{ID: "sweep", Name: "Sweep", Enabled: true, Type: trigger.TypeSchedule, Expect: &trigger.Expect{MaxSilence: time.Hour}},
		{ID: "quiet", Name: "Quiet", Enabled: true, Type: trigger.TypeSchedule, Expect: &trigger.Expect{MaxSilence: time.Hour}},
	}
}

func build(t *testing.T) *report.Report {
	t.Helper()
	runs := src{
		run("mail", 10*time.Minute, "success", `{"outcome":"ok","summary":"Triaged 14 messages.","counts":{"seen":14,"llm":3}}`),
		run("mail", 40*time.Minute, "success", `{"outcome":"ok","summary":"Triaged 6 messages.","counts":{"seen":6}}`),
		run("mail", 70*time.Minute, "success", `{"outcome":"noop","summary":"No unread mail."}`),
		run("sweep", 5*time.Minute, "failure", ""),
		// "quiet" has never run.
	}
	r, err := report.Build(context.Background(), runs, defs(), 24*time.Hour, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestBuildAggregatesReceipts(t *testing.T) {
	r := build(t)
	var mail *report.TriggerLine
	for i := range r.Triggers {
		if r.Triggers[i].TriggerID == "mail" {
			mail = &r.Triggers[i]
		}
	}
	if mail == nil || mail.Runs != 3 || mail.Noops != 1 || mail.Totals["seen"] != 20 || mail.Totals["llm"] != 3 {
		t.Fatalf("mail line: %+v", mail)
	}
	if r.TotalRuns != 4 || r.FailedRuns != 1 {
		t.Fatalf("totals: %d runs, %d failed", r.TotalRuns, r.FailedRuns)
	}
}

func TestAttentionFirstAndHeadline(t *testing.T) {
	r := build(t)
	att := r.NeedsAttention()
	if len(att) != 2 || att[0].TriggerID != "sweep" || att[1].TriggerID != "quiet" {
		t.Fatalf("attention: %+v", att)
	}
	h := r.Headline()
	for _, w := range []string{"24h", "1 failing", "1 silent", "4 runs", "1 failed"} {
		if !strings.Contains(h, w) {
			t.Errorf("headline %q missing %q", h, w)
		}
	}
}

func TestMarkdownAndText(t *testing.T) {
	r := build(t)
	md := r.Markdown()
	for _, w := range []string{"## Needs attention", "`sweep`", "`quiet`", "## Everything else", "| `mail` |", "seen 20"} {
		if !strings.Contains(md, w) {
			t.Errorf("markdown missing %q\n%s", w, md)
		}
	}
	tx := r.Text()
	if !strings.Contains(tx, "failing") || strings.Contains(tx, "|") {
		t.Errorf("text should be plain and mention failing:\n%s", tx)
	}
}

func TestAllHealthyHeadline(t *testing.T) {
	runs := src{run("mail", time.Minute, "success", `{"outcome":"ok","summary":"x"}`)}
	r, _ := report.Build(context.Background(), runs, defs()[:1], 24*time.Hour, now, nil)
	if r.Headline() == "" || len(r.NeedsAttention()) != 0 || !strings.Contains(r.Markdown(), "Nothing needs attention") {
		t.Fatalf("unexpected: %q\n%s", r.Headline(), r.Markdown())
	}
}

func TestFilterByTrigger(t *testing.T) {
	r := build(t)
	f := r.Only("mail")
	if len(f.Triggers) != 1 || f.TotalRuns != 3 {
		t.Fatalf("filtered: %+v", f)
	}
}
