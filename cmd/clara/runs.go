package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/trigger"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:     "run",
	Aliases: []string{"runs"},
	Short:   "Inspect script execution history and audit logs",
}

var (
	runListLimit   int
	runListTrigger string
)

var runListCmd = &cobra.Command{
	Use:   "list",
	Short: "List recent trigger execution runs",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
			Method: ipc.MethodRunList,
			Params: map[string]any{
				"limit":   runListLimit,
				"trigger": runListTrigger,
			},
		})
		if err != nil {
			return err
		}

		if wantJSON() {
			prettyPrint(resp.Data)
			return nil
		}

		data, err := json.Marshal(resp.Data)
		if err != nil {
			return err
		}
		var runs []store.TriggerRunRecord
		if err := json.Unmarshal(data, &runs); err != nil {
			return err
		}

		if len(runs) == 0 {
			fmt.Println("No execution runs found.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "RUN ID\tTRIGGER\tSTATUS\tCODE\tDURATION\tSTARTED AT")
		for _, r := range runs {
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%dms\t%s\n",
				r.ID, r.TriggerID, r.Status, r.ExitCode, r.DurationMs, r.StartedAt.Format("2006-01-02 15:04:05"),
			)
		}
		return w.Flush()
	},
}

var runShowCmd = &cobra.Command{
	Use:   "show <run-id>",
	Short: "Show details and output of a specific run",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
			Method: ipc.MethodRunGet,
			Params: map[string]any{
				"id": args[0],
			},
		})
		if err != nil {
			return err
		}

		if wantJSON() {
			prettyPrint(resp.Data)
			return nil
		}

		data, err := json.Marshal(resp.Data)
		if err != nil {
			return err
		}
		var r store.RunTrace
		if err := json.Unmarshal(data, &r); err != nil {
			return err
		}

		fmt.Printf("Run ID:       %s\n", r.ID)
		fmt.Printf("Trigger ID:   %s (%s)\n", r.TriggerID, r.TriggerType)
		fmt.Printf("Status:       %s\n", r.Status)
		fmt.Printf("Exit Code:    %d\n", r.ExitCode)
		fmt.Printf("Started:      %s\n", r.StartedAt.Format("2006-01-02 15:04:05.000"))
		fmt.Printf("Finished:     %s\n", r.FinishedAt.Format("2006-01-02 15:04:05.000"))
		fmt.Printf("Duration:     %dms\n", r.DurationMs)
		if r.Error != "" {
			fmt.Printf("Error:        %s\n", r.Error)
		}
		printRunLinks(r)
		printRunReceipt(r.Receipt)
		printRunTrace(r)
		if r.EventData != "" {
			fmt.Printf("\n--- Event Data ---\n%s\n", r.EventData)
		}
		if r.Stdout != "" {
			fmt.Printf("\n--- Stdout ---\n%s", r.Stdout)
		}
		if r.Stderr != "" {
			fmt.Printf("\n--- Stderr ---\n%s", r.Stderr)
		}

		return nil
	},
}

var runToolsCmd = &cobra.Command{
	Use:   "tools <run-id>",
	Short: "List MCP tool calls executed during a run",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
			Method: ipc.MethodToolCalls,
			Params: map[string]any{
				"run_id": args[0],
			},
		})
		if err != nil {
			return err
		}

		if wantJSON() {
			prettyPrint(resp.Data)
			return nil
		}

		data, err := json.Marshal(resp.Data)
		if err != nil {
			return err
		}
		var calls []store.ToolCallRecord
		if err := json.Unmarshal(data, &calls); err != nil {
			return err
		}

		if len(calls) == 0 {
			fmt.Printf("No tool calls recorded for run %s.\n", args[0])
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "CALL ID\tTOOL\tDURATION\tERROR\tCALLED AT")
		for _, c := range calls {
			errStr := "-"
			if c.Error != "" {
				errStr = c.Error
			}
			fmt.Fprintf(w, "%s\t%s\t%dms\t%s\t%s\n",
				c.ID, c.ToolName, c.DurationMs, errStr, c.CalledAt.Format("15:04:05.000"),
			)
		}
		return w.Flush()
	},
}

func init() {
	runListCmd.Flags().IntVarP(&runListLimit, "limit", "n", 20, "max number of runs to return")
	runListCmd.Flags().StringVarP(&runListTrigger, "trigger", "t", "", "filter by trigger ID")
	runCmd.AddCommand(runListCmd)
	runCmd.AddCommand(runShowCmd)
	runCmd.AddCommand(runToolsCmd)
}

func printRunLinks(r store.RunTrace) {
	if r.EventID != "" {
		fmt.Printf("Event:        %s\n", r.EventID)
	}
	if r.ParentRunID != "" {
		fmt.Printf("Parent run:   %s\n", r.ParentRunID)
	}
	if r.SessionID != "" {
		fmt.Printf("Agent session: %s\n", r.SessionID)
	}
}

func printRunReceipt(raw string) {
	if raw == "" {
		return
	}
	var rc trigger.Receipt
	if err := json.Unmarshal([]byte(raw), &rc); err != nil {
		return
	}
	kind := "reported by script"
	if rc.Mechanical {
		kind = "mechanical: script reported none"
	}
	fmt.Printf("\n--- Receipt (%s) ---\n", kind)
	fmt.Printf("Outcome:      %s\n%s\n", rc.Outcome, rc.Summary)
	if len(rc.Counts) > 0 {
		keys := make([]string, 0, len(rc.Counts))
		for k := range rc.Counts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s=%d", k, rc.Counts[k]))
		}
		fmt.Printf("Counts:       %s\n", strings.Join(parts, " "))
	}
	for _, a := range rc.Anomalies {
		fmt.Printf("  ! %s\n", a)
	}
}

func printRunTrace(r store.RunTrace) {
	if len(r.ToolCalls) > 0 {
		fmt.Printf("\n--- Tool calls (%d) ---\n", len(r.ToolCalls))
		for _, c := range r.ToolCalls {
			status := "ok"
			if c.Error != "" {
				status = "ERR " + c.Error
			}
			fmt.Printf("  %s  %-28s %5dms  %s\n",
				c.CalledAt.Format("15:04:05.000"), c.ToolName, c.DurationMs, status)
		}
	}
	if len(r.Children) > 0 {
		fmt.Printf("\n--- Started runs ---\n")
		for _, c := range r.Children {
			fmt.Printf("  %s  %s  %s\n", c.ID, c.TriggerID, c.Status)
		}
	}
}
