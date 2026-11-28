package health_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/brightpuddle/clara/internal/health"
	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/trigger"
)

var now = time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

type opt func(*store.TriggerRunRecord)

func receipt(outcome string, counts map[string]int, anomalies ...string) opt {
	return func(r *store.TriggerRunRecord) {
		var parts []string
		for k, v := range counts {
			parts = append(parts, fmt.Sprintf("%q:%d", k, v))
		}
		an := ""
		if len(anomalies) > 0 {
			an = fmt.Sprintf(`,"anomalies":[%q]`, anomalies[0])
		}
		r.Receipt = fmt.Sprintf(`{"outcome":%q,"summary":"s","counts":{%s}%s}`,
			outcome, strings.Join(parts, ","), an)
	}
}

func mechanical() opt {
	return func(r *store.TriggerRunRecord) { r.Receipt = `{"outcome":"ok","summary":"s","mechanical":true}` }
}

func failed() opt      { return func(r *store.TriggerRunRecord) { r.Status = "failure"; r.ExitCode = 1 } }
func dur(ms int64) opt { return func(r *store.TriggerRunRecord) { r.DurationMs = ms } }

// run builds a run that started ago before now, successful unless opts say otherwise.
func run(ago time.Duration, opts ...opt) store.TriggerRunRecord {
	r := store.TriggerRunRecord{
		ID: fmt.Sprintf("r-%d", ago), TriggerID: "t", Status: "success",
		StartedAt: now.Add(-ago), DurationMs: 1000,
	}
	for _, o := range opts {
		o(&r)
	}
	return r
}

func sched(e *trigger.Expect) trigger.Definition {
	return trigger.Definition{ID: "t", Name: "T", Enabled: true, Type: trigger.TypeSchedule, Expect: e}
}

func eval(def trigger.Definition, runs ...store.TriggerRunRecord) health.Result {
	return health.Evaluate(def, runs, now, 24*time.Hour)
}

func want(t *testing.T, r health.Result, v health.Verdict, evidence string) {
	t.Helper()
	if r.Verdict != v {
		t.Fatalf("verdict = %s (%s), want %s", r.Verdict, r.Evidence, v)
	}
	if !strings.Contains(r.Evidence, evidence) {
		t.Fatalf("evidence %q missing %q", r.Evidence, evidence)
	}
}

func TestHealthy(t *testing.T) {
	d := sched(&trigger.Expect{MaxSilence: time.Hour, Outcomes: []string{"ok", "noop"}})
	want(t, eval(d, run(10*time.Minute, receipt("ok", nil)), run(40*time.Minute, receipt("noop", nil))), health.Healthy, "")
}

func TestSilent(t *testing.T) {
	d := sched(&trigger.Expect{MaxSilence: 90 * time.Minute})
	want(t, eval(d, run(3*time.Hour)), health.Silent, "last ran 3h")
	want(t, eval(d), health.Silent, "no runs")
}

func TestSilentNeedsAnExpectation(t *testing.T) {
	want(t, eval(sched(nil)), health.Unknown, "no expect")
}

func TestFailingLastRun(t *testing.T) {
	d := sched(&trigger.Expect{MaxSilence: time.Hour})
	want(t, eval(d, run(5*time.Minute, failed()), run(35*time.Minute)), health.Failing, "last run failed")
}

func TestFailingReportedFailedOutcome(t *testing.T) {
	d := sched(nil)
	want(t, eval(d, run(5*time.Minute, receipt("failed", nil))), health.Failing, "reported failed")
}

func TestFailingWithoutExpectStillReported(t *testing.T) {
	want(t, eval(sched(nil), run(time.Minute, failed())), health.Failing, "last run failed")
}

func TestFailingMajority(t *testing.T) {
	d := sched(&trigger.Expect{})
	want(t, eval(d, run(1*time.Minute), run(2*time.Hour, failed()), run(3*time.Hour, failed())), health.Failing, "2 of 3")
}

func TestDegradedOutcomeNotAllowed(t *testing.T) {
	d := sched(&trigger.Expect{Outcomes: []string{"ok", "noop"}})
	want(t, eval(d, run(time.Minute, receipt("partial", nil))), health.Degraded, "partial")
}

func TestDegradedPartialByDefault(t *testing.T) {
	want(t, eval(sched(&trigger.Expect{}), run(time.Minute, receipt("partial", nil))), health.Degraded, "partial")
}

func TestDegradedSlow(t *testing.T) {
	d := sched(&trigger.Expect{MaxDuration: 30 * time.Second})
	want(t, eval(d, run(time.Minute, dur(45_000))), health.Degraded, "max_duration")
}

func TestDegradedMinActivity(t *testing.T) {
	d := sched(&trigger.Expect{MinActivity: map[string]int{"counts.seen": 5}})
	want(t, eval(d, run(time.Minute, receipt("ok", map[string]int{"seen": 2}))), health.Degraded, "seen")
}

func TestDegradedAnomalies(t *testing.T) {
	d := sched(&trigger.Expect{})
	want(t, eval(d, run(time.Minute, receipt("ok", nil, "LLM fallback used 9 times"))), health.Degraded, "LLM fallback")
}

func TestDegradedLostReceipts(t *testing.T) {
	d := sched(&trigger.Expect{})
	want(t, eval(d, run(time.Minute, mechanical()),
		run(30*time.Hour, receipt("ok", nil)), run(31*time.Hour, receipt("ok", nil)), run(32*time.Hour, receipt("ok", nil))),
		health.Degraded, "no receipt")
}

func TestWorkerRestartStorm(t *testing.T) {
	d := trigger.Definition{ID: "t", Enabled: true, Type: trigger.TypeWorker,
		Expect: &trigger.Expect{MaxRestartsPerDay: 3}}
	var runs []store.TriggerRunRecord
	for i := 1; i <= 5; i++ {
		runs = append(runs, run(time.Duration(i)*time.Hour))
	}
	want(t, eval(d, runs...), health.Degraded, "5 restarts")
}

func TestDisabledIsUnknown(t *testing.T) {
	d := sched(&trigger.Expect{MaxSilence: time.Minute})
	d.Enabled = false
	want(t, eval(d), health.Unknown, "disabled")
}

func TestSlowerThanBaseline(t *testing.T) {
	d := sched(&trigger.Expect{})
	runs := []store.TriggerRunRecord{run(time.Hour, dur(20_000))}
	for i := 2; i <= 8; i++ {
		runs = append(runs, run(time.Duration(i)*24*time.Hour/2*3, dur(2_000)))
	}
	want(t, eval(d, runs...), health.Degraded, "slower than usual")
}

type fakeSource []store.TriggerRunRecord

func (f fakeSource) ListRunSummariesSince(_ context.Context, since time.Time) ([]store.TriggerRunRecord, error) {
	var out []store.TriggerRunRecord
	for _, r := range f {
		if !r.StartedAt.Before(since) {
			out = append(out, r)
		}
	}
	return out, nil
}

func TestReportSortsAttentionFirst(t *testing.T) {
	good := sched(&trigger.Expect{})
	good.ID = "a-good"
	bad := sched(&trigger.Expect{})
	bad.ID = "z-bad"
	mk := func(id string, opts ...opt) store.TriggerRunRecord {
		r := run(time.Minute, opts...)
		r.TriggerID = id
		return r
	}
	got, err := health.Report(context.Background(),
		fakeSource{mk("a-good"), mk("z-bad", failed())},
		[]trigger.Definition{good, bad}, 24*time.Hour, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].TriggerID != "z-bad" || got[0].Verdict != health.Failing || got[1].Verdict != health.Healthy {
		t.Fatalf("unexpected order: %+v", got)
	}
}

func TestSubSecondDurationsAreNotSlower(t *testing.T) {
	d := sched(&trigger.Expect{})
	runs := []store.TriggerRunRecord{run(time.Hour, dur(300))}
	for i := 2; i <= 8; i++ {
		runs = append(runs, run(time.Duration(i)*24*time.Hour/2*3, dur(100)))
	}
	if r := eval(d, runs...); r.Verdict == health.Degraded {
		t.Fatalf("sub-second jitter flagged: %s", r.Evidence)
	}
}

func TestCancelledRunsAreNotFailures(t *testing.T) {
	d := sched(&trigger.Expect{})
	cancelled := func() opt { return func(r *store.TriggerRunRecord) { r.Status = "cancelled"; r.ExitCode = -1 } }
	r := eval(d, run(time.Minute, cancelled()), run(time.Hour))
	if r.Verdict != health.Healthy || r.Failures != 0 {
		t.Fatalf("cancelled counted as failure: %+v", r)
	}
}

func TestRunningWorkerIsHealthyDespiteOldFailure(t *testing.T) {
	d := trigger.Definition{ID: "t", Name: "T", Enabled: true, Type: trigger.TypeWorker, Expect: &trigger.Expect{}}
	old := []store.TriggerRunRecord{run(12*time.Hour, failed())}
	since := now.Add(-11 * time.Hour)
	r := health.EvaluateActive(d, old, now, 24*time.Hour, &since)
	if r.Verdict != health.Healthy || !strings.Contains(r.Evidence, "running") {
		t.Fatalf("running worker: %s (%s)", r.Verdict, r.Evidence)
	}
	if r2 := health.EvaluateActive(d, old, now, 24*time.Hour, nil); r2.Verdict != health.Failing {
		t.Fatalf("stopped worker with a failed last run should fail, got %s", r2.Verdict)
	}
}
