package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/brightpuddle/clara/internal/report"
)

var (
	reportSince   string
	reportTrigger string
	reportFormat  string
)

var reportCmd = &cobra.Command{
	Use:   "report",
	Short: "What Clara did, and whether it is working",
	Long: `Prints a short account of recent activity: triggers that need attention first, then
one line per trigger with what it did. Use "clara run show <id>" for detail on a run.`,
	Args: cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
			Method: ipc.MethodReport,
			Params: map[string]any{"since": reportSince, "trigger": reportTrigger},
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
		var rep report.Rendered
		if err := json.Unmarshal(raw, &rep); err != nil {
			return err
		}
		if reportFormat == "markdown" {
			fmt.Print(rep.Markdown)
		} else {
			fmt.Print(rep.Text)
		}
		return nil
	},
}

func init() {
	reportCmd.Flags().StringVar(&reportSince, "since", "24h", "window to cover (e.g. 6h, 24h, 72h)")
	reportCmd.Flags().StringVarP(&reportTrigger, "trigger", "t", "", "limit to one trigger ID")
	reportCmd.Flags().StringVar(&reportFormat, "format", "text", "text or markdown")
}
