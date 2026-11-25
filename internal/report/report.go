// Package report turns run history and health verdicts into prose a person reads in two
// minutes. It is templated: no LLM, and it never reads raw logs.
package report

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/brightpuddle/clara/internal/health"
	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/trigger"
)

// TriggerLine is one trigger's verdict plus what it did in the window.
type TriggerLine struct {
	health.Result
	Noops    int            `json:"noops"`
	Partials int            `json:"partials"`
	Totals   map[string]int `json:"totals,omitempty"` // receipt counts summed over the window
}

// Report is the structured account of a window of Clara activity.
type Report struct {
	Window     time.Duration `json:"window"`
	Until      time.Time     `json:"until"`
	TotalRuns  int           `json:"total_runs"`
	FailedRuns int           `json:"failed_runs"`
	Triggers   []TriggerLine `json:"triggers"`
}

// Build evaluates every definition over window and aggregates receipts.
func Build(
	ctx context.Context, src health.RunSource, defs []trigger.Definition,
	window time.Duration, now time.Time,
) (*Report, error) {
	runs, err := src.ListRunSummariesSince(ctx, now.Add(-health.HistoryWindow(defs)))
	if err != nil {
		return nil, err
	}
	byTrigger := map[string][]store.TriggerRunRecord{}
	for _, r := range runs {
		byTrigger[r.TriggerID] = append(byTrigger[r.TriggerID], r)
	}

	rep := &Report{Window: window, Until: now}
	for _, d := range defs {
		hist := byTrigger[d.ID]
		line := TriggerLine{Result: health.Evaluate(d, hist, now, window), Totals: map[string]int{}}
		for _, r := range hist {
			if now.Sub(r.StartedAt) > window {
				continue
			}
			var rc trigger.Receipt
			if r.Receipt != "" && json.Unmarshal([]byte(r.Receipt), &rc) == nil && !rc.Mechanical {
				switch rc.Outcome {
				case trigger.OutcomeNoop:
					line.Noops++
				case trigger.OutcomePartial:
					line.Partials++
				}
				for k, v := range rc.Counts {
					line.Totals[k] += v
				}
			}
		}
		rep.Triggers = append(rep.Triggers, line)
	}
	rep.sort()
	rep.recount()
	return rep, nil
}

func (r *Report) sort() {
	sort.SliceStable(r.Triggers, func(i, j int) bool {
		a, b := r.Triggers[i], r.Triggers[j]
		if a.Verdict.Rank() != b.Verdict.Rank() {
			return a.Verdict.Rank() > b.Verdict.Rank()
		}
		return a.TriggerID < b.TriggerID
	})
}

func (r *Report) recount() {
	r.TotalRuns, r.FailedRuns = 0, 0
	for _, t := range r.Triggers {
		r.TotalRuns += t.Runs
		r.FailedRuns += t.Failures
	}
}

// Only returns a copy limited to one trigger.
func (r *Report) Only(id string) *Report {
	out := &Report{Window: r.Window, Until: r.Until}
	for _, t := range r.Triggers {
		if t.TriggerID == id {
			out.Triggers = append(out.Triggers, t)
		}
	}
	out.recount()
	return out
}

func attention(v health.Verdict) bool {
	return v == health.Failing || v == health.Silent || v == health.Degraded
}

// NeedsAttention lists failing, silent and degraded triggers, worst first.
func (r *Report) NeedsAttention() []TriggerLine {
	var out []TriggerLine
	for _, t := range r.Triggers {
		if attention(t.Verdict) {
			out = append(out, t)
		}
	}
	return out
}

// Headline is a one-sentence summary suitable for a notification.
func (r *Report) Headline() string {
	counts := map[health.Verdict]int{}
	for _, t := range r.Triggers {
		counts[t.Verdict]++
	}
	var bad []string
	for _, v := range []health.Verdict{health.Failing, health.Silent, health.Degraded} {
		if counts[v] > 0 {
			bad = append(bad, fmt.Sprintf("%d %s", counts[v], v))
		}
	}
	state := "everything healthy"
	if len(bad) > 0 {
		state = strings.Join(bad, ", ")
	}
	return fmt.Sprintf("Clara, last %s: %s; %d runs, %d failed.",
		short(r.Window), state, r.TotalRuns, r.FailedRuns)
}

// Markdown renders the report body for the vault (no frontmatter).
func (r *Report) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "> %s\n\n", r.Headline())

	b.WriteString("## Needs attention\n\n")
	att := r.NeedsAttention()
	if len(att) == 0 {
		b.WriteString("Nothing needs attention.\n")
	}
	for _, t := range att {
		fmt.Fprintf(&b, "- **%s** `%s`: %s\n", t.Verdict, t.TriggerID, t.Evidence)
	}

	b.WriteString("\n## Everything else\n\n")
	b.WriteString("| Trigger | Verdict | Runs | Failed | What it did |\n|---|---|---|---|---|\n")
	for _, t := range r.Triggers {
		if attention(t.Verdict) {
			continue
		}
		fmt.Fprintf(&b, "| `%s` | %s | %d | %d | %s |\n",
			t.TriggerID, t.Verdict, t.Runs, t.Failures, cell(didWhat(t)))
	}
	return b.String()
}

// Text renders the report for a terminal: attention first, then one line per trigger.
func (r *Report) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", r.Headline())
	att := r.NeedsAttention()
	if len(att) > 0 {
		b.WriteString("Needs attention:\n")
		for _, t := range att {
			fmt.Fprintf(&b, "  %-9s %s  %s\n", t.Verdict, t.TriggerID, t.Evidence)
		}
		b.WriteString("\n")
	}
	b.WriteString("Everything else:\n")
	for _, t := range r.Triggers {
		if attention(t.Verdict) {
			continue
		}
		fmt.Fprintf(&b, "  %-9s %s  %d run(s)  %s\n", t.Verdict, t.TriggerID, t.Runs, didWhat(t))
	}
	return b.String()
}

func didWhat(t TriggerLine) string {
	parts := []string{}
	if t.LastSummary != "" {
		parts = append(parts, t.LastSummary)
	} else if t.Evidence != "" {
		parts = append(parts, t.Evidence)
	}
	if len(t.Totals) > 0 && t.Runs > 1 {
		keys := make([]string, 0, len(t.Totals))
		for k := range t.Totals {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		tot := make([]string, 0, len(keys))
		for _, k := range keys {
			tot = append(tot, fmt.Sprintf("%s %d", k, t.Totals[k]))
		}
		parts = append(parts, "Window totals: "+strings.Join(tot, ", ")+".")
	}
	if t.Noops > 0 {
		parts = append(parts, fmt.Sprintf("%d run(s) had nothing to do.", t.Noops))
	}
	return strings.Join(parts, " ")
}

func cell(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "|", "/"), "\n", " ") }

func short(d time.Duration) string {
	if d >= time.Hour && d%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	if d >= time.Minute && d%time.Minute == 0 {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return d.String()
}

// Rendered is a Report together with its prose forms, for tools and IPC.
type Rendered struct {
	Report
	Headline  string        `json:"headline"`
	Markdown  string        `json:"markdown"`
	Text      string        `json:"text"`
	Attention []TriggerLine `json:"attention"`
}

// Render fills in every prose form.
func (r *Report) Render() Rendered {
	return Rendered{
		Report: *r, Headline: r.Headline(), Markdown: r.Markdown(),
		Text: r.Text(), Attention: r.NeedsAttention(),
	}
}

// ParseWindow parses a --since value, defaulting to 24h.
func ParseWindow(s string) (time.Duration, error) {
	if s == "" {
		return 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid since duration %q", s)
	}
	return d, nil
}

// BuildFor builds the report and optionally narrows it to one trigger.
func BuildFor(
	ctx context.Context, src health.RunSource, defs []trigger.Definition,
	since, triggerID string, now time.Time,
) (*Report, error) {
	window, err := ParseWindow(since)
	if err != nil {
		return nil, err
	}
	r, err := Build(ctx, src, defs, window, now)
	if err != nil {
		return nil, err
	}
	if triggerID != "" {
		r = r.Only(triggerID)
		if len(r.Triggers) == 0 {
			return nil, fmt.Errorf("trigger %q not found", triggerID)
		}
	}
	return r, nil
}
