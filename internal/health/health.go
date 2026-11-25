// Package health judges each trigger against its declared expectations and its own history.
package health

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/trigger"
)

// Verdict is the overall health of one trigger.
type Verdict string

const (
	Healthy  Verdict = "healthy"
	Degraded Verdict = "degraded" // slower, noisier or less complete than expected
	Failing  Verdict = "failing"
	Silent   Verdict = "silent" // should have run and didn't
	Unknown  Verdict = "unknown"
)

// Rank orders verdicts by how much attention they need (higher first).
func (v Verdict) Rank() int {
	switch v {
	case Failing:
		return 4
	case Silent:
		return 3
	case Degraded:
		return 2
	case Unknown:
		return 1
	}
	return 0
}

// Result is one trigger's verdict with the evidence behind it.
type Result struct {
	TriggerID   string    `json:"trigger_id"`
	Name        string    `json:"name"`
	Verdict     Verdict   `json:"verdict"`
	Evidence    string    `json:"evidence"`
	Runs        int       `json:"runs"`
	Failures    int       `json:"failures"`
	LastRun     time.Time `json:"last_run,omitempty"`
	LastSummary string    `json:"last_summary,omitempty"`
}

const (
	minBaselineRuns = 5
	slowFactor      = 2.0
)

// Evaluate judges def. runs may include history older than window; it is used as
// the baseline. Order does not matter.
func Evaluate(
	def trigger.Definition, runs []store.TriggerRunRecord, now time.Time, window time.Duration,
) Result {
	res := Result{TriggerID: def.ID, Name: def.Name, Verdict: Healthy}

	sorted := append([]store.TriggerRunRecord(nil), runs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].StartedAt.After(sorted[j].StartedAt) })

	var recent, base []store.TriggerRunRecord
	for _, r := range sorted {
		if now.Sub(r.StartedAt) <= window {
			recent = append(recent, r)
		} else {
			base = append(base, r)
		}
	}
	res.Runs = len(recent)
	for _, r := range recent {
		if isFailure(r) {
			res.Failures++
		}
	}
	if len(sorted) > 0 {
		res.LastRun = sorted[0].StartedAt
		if rc := parseReceipt(sorted[0]); rc != nil {
			res.LastSummary = rc.Summary
		}
	}

	if !def.Enabled {
		return finish(res, Unknown, "disabled")
	}
	exp := def.Expect

	// Silence is judged on all history: the last run may be outside the window.
	if exp != nil && exp.MaxSilence > 0 {
		if len(sorted) == 0 {
			return finish(res, Silent, fmt.Sprintf("no runs recorded; expected within %s", short(exp.MaxSilence)))
		}
		if age := now.Sub(sorted[0].StartedAt); age > exp.MaxSilence {
			return finish(res, Silent, fmt.Sprintf("last ran %s ago, expected within %s", short(age), short(exp.MaxSilence)))
		}
	}

	if len(recent) == 0 {
		if exp == nil {
			return finish(res, Unknown, "no expect declared and no runs in window")
		}
		return finish(res, Healthy, "no runs in window")
	}

	latest := recent[0]
	if isFailure(latest) {
		why := fmt.Sprintf("last run failed (%s) %s ago", failReason(latest), short(now.Sub(latest.StartedAt)))
		return finish(res, Failing, why)
	}
	if rc := parseReceipt(latest); rc != nil && rc.Outcome == trigger.OutcomeFailed {
		return finish(res, Failing, "last run reported failed: "+rc.Summary)
	}
	if len(recent) >= 2 && res.Failures*2 >= len(recent) {
		return finish(res, Failing, fmt.Sprintf("%d of %d runs failed in the window", res.Failures, len(recent)))
	}

	var issues []string
	issues = append(issues, outcomeIssues(exp, recent)...)
	issues = append(issues, durationIssues(exp, recent, base)...)
	if exp != nil {
		issues = append(issues, activityIssues(exp, latest)...)
		issues = append(issues, restartIssues(def, exp, sorted, now)...)
	}
	issues = append(issues, anomalyIssues(latest)...)
	issues = append(issues, lostReceiptIssues(latest, base)...)
	issues = append(issues, errorRateIssues(recent, base)...)

	if len(issues) > 0 {
		return finish(res, Degraded, strings.Join(issues, "; "))
	}
	if exp == nil {
		return finish(res, Unknown, "no expect declared; runs look fine")
	}
	return finish(res, Healthy, "")
}

func finish(r Result, v Verdict, evidence string) Result {
	r.Verdict, r.Evidence = v, evidence
	return r
}

func isFailure(r store.TriggerRunRecord) bool {
	return r.Status == string(trigger.StatusFailure) || r.Status == string(trigger.StatusTimeout)
}

func failReason(r store.TriggerRunRecord) string {
	if r.Status == string(trigger.StatusTimeout) {
		return "timeout"
	}
	return fmt.Sprintf("exit %d", r.ExitCode)
}

func parseReceipt(r store.TriggerRunRecord) *trigger.Receipt {
	if r.Receipt == "" {
		return nil
	}
	var rc trigger.Receipt
	if json.Unmarshal([]byte(r.Receipt), &rc) != nil {
		return nil
	}
	return &rc
}

func outcomeIssues(exp *trigger.Expect, recent []store.TriggerRunRecord) []string {
	allowed := map[string]bool{}
	if exp != nil {
		for _, o := range exp.Outcomes {
			allowed[o] = true
		}
	}
	bad := map[string]int{}
	for _, r := range recent {
		rc := parseReceipt(r)
		if rc == nil || rc.Mechanical {
			continue
		}
		if len(allowed) > 0 && !allowed[rc.Outcome] || len(allowed) == 0 && rc.Outcome == trigger.OutcomePartial {
			bad[rc.Outcome]++
		}
	}
	var out []string
	for o, n := range bad {
		out = append(out, fmt.Sprintf("%d run(s) with outcome %s", n, o))
	}
	sort.Strings(out)
	return out
}

func durationIssues(exp *trigger.Expect, recent, base []store.TriggerRunRecord) []string {
	var out []string
	if exp != nil && exp.MaxDuration > 0 {
		over := 0
		for _, r := range recent {
			if time.Duration(r.DurationMs)*time.Millisecond > exp.MaxDuration {
				over++
			}
		}
		if over > 0 {
			out = append(out, fmt.Sprintf("%d run(s) over max_duration %s", over, short(exp.MaxDuration)))
		}
	}
	if len(base) >= minBaselineRuns {
		bm, rm := median(durations(base)), median(durations(recent))
		if bm > 0 && rm > bm*slowFactor {
			out = append(out, fmt.Sprintf("slower than usual: median %s vs %s", short(rm), short(bm)))
		}
	}
	return out
}

func activityIssues(exp *trigger.Expect, latest store.TriggerRunRecord) []string {
	rc := parseReceipt(latest)
	if rc == nil || rc.Mechanical || len(exp.MinActivity) == 0 {
		return nil
	}
	var out []string
	keys := make([]string, 0, len(exp.MinActivity))
	for k := range exp.MinActivity {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		name := strings.TrimPrefix(k, "counts.")
		if got := rc.Counts[name]; got < exp.MinActivity[k] {
			out = append(out, fmt.Sprintf("%s was %d, expected at least %d", name, got, exp.MinActivity[k]))
		}
	}
	return out
}

func restartIssues(
	def trigger.Definition, exp *trigger.Expect, all []store.TriggerRunRecord, now time.Time,
) []string {
	if def.Type != trigger.TypeWorker || exp.MaxRestartsPerDay <= 0 {
		return nil
	}
	n := 0
	for _, r := range all {
		if now.Sub(r.StartedAt) <= 24*time.Hour {
			n++
		}
	}
	if n > exp.MaxRestartsPerDay {
		return []string{fmt.Sprintf("%d restarts in 24h, limit %d", n, exp.MaxRestartsPerDay)}
	}
	return nil
}

func anomalyIssues(latest store.TriggerRunRecord) []string {
	rc := parseReceipt(latest)
	if rc == nil {
		return nil
	}
	return rc.Anomalies
}

// lostReceiptIssues flags a script that used to report but now doesn't.
func lostReceiptIssues(latest store.TriggerRunRecord, base []store.TriggerRunRecord) []string {
	rc := parseReceipt(latest)
	if rc == nil || !rc.Mechanical {
		return nil
	}
	reported := 0
	for _, r := range base {
		if b := parseReceipt(r); b != nil && !b.Mechanical {
			reported++
		}
	}
	if reported >= 3 {
		return []string{"no receipt from a script that usually reports one"}
	}
	return nil
}

func errorRateIssues(recent, base []store.TriggerRunRecord) []string {
	if len(base) < minBaselineRuns || len(recent) < 3 {
		return nil
	}
	rate := func(rs []store.TriggerRunRecord) float64 {
		n := 0
		for _, r := range rs {
			if isFailure(r) {
				n++
			}
		}
		return float64(n) / float64(len(rs))
	}
	if rr, br := rate(recent), rate(base); rr > br+0.2 {
		return []string{fmt.Sprintf("error rate %.0f%% vs usual %.0f%%", rr*100, br*100)}
	}
	return nil
}

func durations(rs []store.TriggerRunRecord) []time.Duration {
	out := make([]time.Duration, len(rs))
	for i, r := range rs {
		out[i] = time.Duration(r.DurationMs) * time.Millisecond
	}
	return out
}

func median(ds []time.Duration) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	c := append([]time.Duration(nil), ds...)
	sort.Slice(c, func(i, j int) bool { return c[i] < c[j] })
	return c[len(c)/2]
}

func short(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d >= time.Hour:
		h, m := int(d.Hours()), int(d.Minutes())%60
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh%dm", h, m)
	case d >= time.Minute:
		m, s := int(d.Minutes()), int(d.Seconds())%60
		if s == 0 {
			return fmt.Sprintf("%dm", m)
		}
		return fmt.Sprintf("%dm%ds", m, s)
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

// RunSource is the part of the audit store Report needs.
type RunSource interface {
	ListRunSummariesSince(ctx context.Context, since time.Time) ([]store.TriggerRunRecord, error)
}

const minBaselineWindow = 14 * 24 * time.Hour

// Report evaluates every definition over window, sorted so the triggers needing
// attention come first.
func Report(
	ctx context.Context, src RunSource, defs []trigger.Definition, window time.Duration, now time.Time,
) ([]Result, error) {
	history := minBaselineWindow
	for _, d := range defs {
		if d.Expect != nil && 2*d.Expect.MaxSilence > history {
			history = 2 * d.Expect.MaxSilence
		}
	}
	runs, err := src.ListRunSummariesSince(ctx, now.Add(-history))
	if err != nil {
		return nil, err
	}
	byTrigger := map[string][]store.TriggerRunRecord{}
	for _, r := range runs {
		byTrigger[r.TriggerID] = append(byTrigger[r.TriggerID], r)
	}

	out := make([]Result, 0, len(defs))
	for _, d := range defs {
		out = append(out, Evaluate(d, byTrigger[d.ID], now, window))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if ri, rj := out[i].Verdict.Rank(), out[j].Verdict.Rank(); ri != rj {
			return ri > rj
		}
		return out[i].TriggerID < out[j].TriggerID
	})
	return out, nil
}
