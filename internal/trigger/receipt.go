package trigger

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/brightpuddle/clara/internal/store"
)

// Receipt outcomes. noop is a first-class result: the run worked and found nothing to do.
const (
	OutcomeOK      = "ok"
	OutcomePartial = "partial"
	OutcomeFailed  = "failed"
	OutcomeNoop    = "noop"
)

// Receipt is the human-scale account of one run. Scripts write it through
// run.report; when they do not, Clara synthesises a mechanical one.
type Receipt struct {
	Outcome   string         `json:"outcome"`
	Summary   string         `json:"summary"`
	Counts    map[string]int `json:"counts,omitempty"`
	Anomalies []string       `json:"anomalies,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	// Mechanical is true when Clara built the receipt because the script reported none.
	Mechanical bool `json:"mechanical,omitempty"`
}

// Validate checks that a script-supplied receipt is well formed.
func (r Receipt) Validate() error {
	switch r.Outcome {
	case OutcomeOK, OutcomePartial, OutcomeFailed, OutcomeNoop:
	default:
		return errors.Newf("invalid outcome %q: want ok, partial, failed or noop", r.Outcome)
	}
	if strings.TrimSpace(r.Summary) == "" {
		return errors.New("summary is required")
	}
	return nil
}

// MechanicalReceipt describes a run from facts Clara already has: exit status,
// duration and the tool calls recorded under the run.
func MechanicalReceipt(rec *RunRecord, calls []store.ToolCallRecord) Receipt {
	byTool := map[string]int{}
	errCount := 0
	for _, c := range calls {
		byTool[c.ToolName]++
		if c.Error != "" {
			errCount++
		}
	}

	r := Receipt{
		Mechanical: true,
		Counts:     map[string]int{"tool_calls": len(calls), "tool_errors": errCount},
	}

	dur := fmt.Sprintf("%.1fs", float64(rec.DurationMs)/1000)
	switch rec.Status {
	case StatusSuccess:
		r.Outcome = OutcomeOK
		r.Summary = fmt.Sprintf("Exited 0 in %s.", dur)
	case StatusTimeout:
		r.Outcome = OutcomeFailed
		r.Summary = fmt.Sprintf("Timed out after %s (%s).", dur, rec.Error)
	default:
		r.Outcome = OutcomeFailed
		r.Summary = fmt.Sprintf("Failed with exit code %d after %s.", rec.ExitCode, dur)
		if rec.Error != "" {
			r.Summary += " " + rec.Error
		}
	}

	if len(calls) == 0 {
		r.Summary += " No tool calls."
	} else {
		names := make([]string, 0, len(byTool))
		for n := range byTool {
			names = append(names, n)
		}
		sort.Strings(names)
		parts := make([]string, 0, len(names))
		for _, n := range names {
			parts = append(parts, fmt.Sprintf("%s×%d", n, byTool[n]))
		}
		r.Summary += fmt.Sprintf(" %d tool calls (%s)", len(calls), strings.Join(parts, ", "))
		if errCount > 0 {
			r.Summary += fmt.Sprintf(", %d failed", errCount)
			r.Anomalies = append(r.Anomalies, fmt.Sprintf("%d tool call(s) errored", errCount))
		}
		r.Summary += "."
	}
	return r
}
