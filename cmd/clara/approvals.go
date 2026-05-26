package main

import (
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// `clara approvals` command group
// ---------------------------------------------------------------------------

var approvalsCmd = &cobra.Command{
	Use:   "approvals",
	Short: "Manage human-in-the-loop approval requests",
}

var approvalsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List pending approval requests",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := loadConfig(); err != nil {
			return err
		}
		resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{Method: ipc.MethodApprovalList})
		if err != nil {
			return err
		}
		items, _ := resp.Data.([]any)
		if len(items) == 0 {
			fmt.Println("No pending approvals.")
			return nil
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(tw, "ID\tCONTEXT")
		for _, item := range items {
			m, _ := item.(map[string]any)
			fmt.Fprintf(tw, "%s\t%s\n", m["request_id"], m["context"])
		}
		tw.Flush()
		return nil
	},
}

var approvalsShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show details and options for an approval request",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := loadConfig(); err != nil {
			return err
		}
		resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
			Method: ipc.MethodApprovalShow,
			Params: map[string]any{"id": args[0]},
		})
		if err != nil {
			return err
		}
		m, _ := resp.Data.(map[string]any)
		if m == nil {
			return fmt.Errorf("approval %q not found", args[0])
		}
		fmt.Printf("Approval Request: %s\n", m["request_id"])
		fmt.Printf("Context: %s\n\n", m["context"])
		fmt.Println("Options:")
		opts, _ := m["options"].([]any)
		for i, opt := range opts {
			o, _ := opt.(map[string]any)
			fmt.Printf("  [%d] %s\n", i+1, o["description"])
		}
		return nil
	},
}

var approvalsDecideCmd = &cobra.Command{
	Use:   "decide <id> <option-number>",
	Short: "Submit a decision for a pending approval request",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := loadConfig(); err != nil {
			return err
		}
		optNum, err := strconv.Atoi(args[1])
		if err != nil || optNum < 1 {
			return fmt.Errorf("option-number must be a positive integer")
		}
		resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
			Method: ipc.MethodApprovalDecide,
			Params: map[string]any{
				"id":     args[0],
				"option": optNum,
			},
		})
		if err != nil {
			return err
		}
		fmt.Println(resp.Message)
		return nil
	},
}

// ---------------------------------------------------------------------------
// `clara request` command
// ---------------------------------------------------------------------------

var requestCmd = &cobra.Command{
	Use:   "request <prompt>",
	Short: "Dispatch a natural-language prompt to the evaluator",
	Long: `Formats and dispatches a clara.user.prompt CloudEvent.
The Evaluator intercepts it; if no matching Actuator is found, it enters
Builder Mode and generates a new compiled Actuator automatically.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := loadConfig(); err != nil {
			return err
		}
		resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
			Method: ipc.MethodRequest,
			Params: map[string]any{"prompt": args[0]},
		})
		if err != nil {
			return err
		}
		fmt.Println(resp.Message)
		return nil
	},
}

func init() {
	approvalsCmd.AddCommand(approvalsListCmd, approvalsShowCmd, approvalsDecideCmd)
	rootCmd.AddCommand(approvalsCmd, requestCmd)
}
