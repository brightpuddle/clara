package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/brightpuddle/clara/internal/health"
	"github.com/brightpuddle/clara/internal/ipc"
)

var healthSince string

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Judge every trigger against its declared expectations and history",
	Long: `Reports healthy, degraded, failing, silent or unknown for each trigger, with the
evidence in one line. Expectations come from the optional "expect:" block in the trigger
YAML; triggers without one are only judged on obvious failures.`,
	Args: cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
			Method: ipc.MethodHealth,
			Params: map[string]any{"since": healthSince},
		})
		if err != nil {
			return err
		}
		if wantJSON() {
			prettyPrint(resp.Data)
			return nil
		}
		raw, err := json.Marshal(resp.Data)
		if err != nil {
			return err
		}
		var results []health.Result
		if err := json.Unmarshal(raw, &results); err != nil {
			return err
		}
		printHealth(results, healthSince)
		return nil
	},
}

func printHealth(results []health.Result, since string) {
	counts := map[health.Verdict]int{}
	for _, r := range results {
		counts[r.Verdict]++
	}
	if since == "" {
		since = "24h"
	}
	fmt.Printf("Health over the last %s: %d failing, %d silent, %d degraded, %d healthy, %d unknown\n\n",
		since, counts[health.Failing], counts[health.Silent], counts[health.Degraded],
		counts[health.Healthy], counts[health.Unknown])

	for _, r := range results {
		line := fmt.Sprintf("%-9s %s", r.Verdict, r.TriggerID)
		switch {
		case r.Evidence != "":
			line += "  " + r.Evidence
		case r.LastSummary != "":
			line += "  " + r.LastSummary
		}
		if r.Runs > 0 && r.Verdict != health.Healthy {
			line += fmt.Sprintf(" (%d run(s), %d failed)", r.Runs, r.Failures)
		}
		if !r.LastRun.IsZero() && r.Verdict == health.Healthy {
			line += fmt.Sprintf("  [last %s ago]", time.Since(r.LastRun).Round(time.Minute))
		}
		fmt.Println(strings.TrimRight(line, " "))
	}
}

func init() {
	healthCmd.Flags().StringVar(&healthSince, "since", "24h", "window to judge (e.g. 6h, 24h, 72h)")
}
