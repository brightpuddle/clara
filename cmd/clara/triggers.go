package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/brightpuddle/clara/internal/trigger"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
)

var triggerCmd = &cobra.Command{
	Use:     "trigger",
	Aliases: []string{"triggers"},
	Short:   "Inspect and execute configured triggers",
}

var triggerListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all loaded triggers",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
			Method: ipc.MethodTriggerList,
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
		var triggers []trigger.Definition
		if err := json.Unmarshal(data, &triggers); err != nil {
			return err
		}

		if len(triggers) == 0 {
			fmt.Println("No triggers configured.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tTYPE\tENABLED\tSCHEDULE/RULE\tACTION EXEC")
		for _, t := range triggers {
			ruleDesc := "-"
			if t.Type == trigger.TypeSchedule {
				ruleDesc = t.Schedule
			} else if t.Type == trigger.TypeEvent {
				if t.Match != nil && t.Match.Field != "" {
					ruleDesc = fmt.Sprintf("%s %s %v", t.Match.Field, t.Match.Op, t.Match.Value)
				} else if t.Match != nil {
					ruleDesc = "compound rule"
				} else {
					ruleDesc = "match all"
				}
			} else if t.Type == trigger.TypeWorker {
				ruleDesc = fmt.Sprintf("restart:%s", t.Action.Restart)
			} else if t.Type == trigger.TypeManual {
				ruleDesc = "manual"
			}

			fmt.Fprintf(w, "%s\t%s\t%s\t%v\t%s\t%s\n",
				t.ID, t.Name, t.Type, t.Enabled, ruleDesc, t.Action.Exec,
			)
		}
		return w.Flush()
	},
}

var (
	testEventJSON string
	testEventFile string
)

var triggerTestCmd = &cobra.Command{
	Use:   "test <trigger-id>",
	Short: "Test if an event matches a trigger's rules",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		eventData, err := loadEventInput(testEventJSON, testEventFile)
		if err != nil {
			return err
		}

		resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
			Method: ipc.MethodTriggerTest,
			Params: map[string]any{
				"id":    args[0],
				"event": eventData,
			},
		})
		if err != nil {
			return err
		}

		if wantJSON() {
			prettyPrint(resp.Data)
			return nil
		}

		matched, _ := resp.Data.(bool)
		if matched {
			fmt.Printf("✓ Event MATCHES trigger %q\n", args[0])
		} else {
			fmt.Printf("✗ Event DOES NOT MATCH trigger %q\n", args[0])
		}
		return nil
	},
}

var (
	runEventJSON string
	runEventFile string
)

var triggerRunCmd = &cobra.Command{
	Use:   "run <trigger-id>",
	Short: "Manually execute a trigger action",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		eventData, err := loadEventInput(runEventJSON, runEventFile)
		if err != nil {
			return err
		}

		resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
			Method: ipc.MethodTriggerRun,
			Params: map[string]any{
				"id":    args[0],
				"event": eventData,
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
		var record trigger.RunRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return err
		}

		fmt.Printf("Run ID:      %s\n", record.ID)
		fmt.Printf("Status:      %s\n", record.Status)
		fmt.Printf("Exit Code:   %d\n", record.ExitCode)
		fmt.Printf("Duration:    %dms\n", record.DurationMs)
		if record.Error != "" {
			fmt.Printf("Error:       %s\n", record.Error)
		}
		if record.Stdout != "" {
			fmt.Printf("\n--- Stdout ---\n%s", record.Stdout)
		}
		if record.Stderr != "" {
			fmt.Printf("\n--- Stderr ---\n%s", record.Stderr)
		}
		return nil
	},
}

func loadEventInput(jsonStr, filePath string) (any, error) {
	if filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, errors.Wrapf(err, "read event file: %s", filePath)
		}
		var parsed any
		if err := json.Unmarshal(data, &parsed); err != nil {
			return nil, errors.Wrap(err, "parse event file JSON")
		}
		return parsed, nil
	}
	if jsonStr != "" {
		var parsed any
		if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
			return nil, errors.Wrap(err, "parse event JSON")
		}
		return parsed, nil
	}
	return map[string]any{}, nil
}

func init() {
	triggerCmd.AddCommand(triggerListCmd)

	triggerTestCmd.Flags().StringVar(&testEventJSON, "event", "", "event payload JSON string")
	triggerTestCmd.Flags().StringVarP(&testEventFile, "file", "f", "", "path to JSON event file")
	triggerCmd.AddCommand(triggerTestCmd)

	triggerRunCmd.Flags().StringVar(&runEventJSON, "event", "", "event payload JSON string")
	triggerRunCmd.Flags().StringVarP(&runEventFile, "file", "f", "", "path to JSON event file")
	triggerCmd.AddCommand(triggerRunCmd)
}
