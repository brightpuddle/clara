package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
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

var approvalsSubmitCmd = &cobra.Command{
	Use:   "submit [id] [context]",
	Short: "Submit a dummy approval request for manual testing",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := loadConfig(); err != nil {
			return err
		}
		id := "dummy-approval"
		contextStr := "Dummy approval request for manual HITL testing"
		if len(args) > 0 {
			id = args[0]
		}
		if len(args) > 1 {
			contextStr = args[1]
		}
		resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
			Method: ipc.MethodApprovalSubmit,
			Params: map[string]any{
				"id":      id,
				"context": contextStr,
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

// ---------------------------------------------------------------------------
// `clara chat` interactive REPL command
// ---------------------------------------------------------------------------

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Start an interactive dialog session to plan and create/update automations",
	Long: `Starts a Read-Eval-Print Loop (REPL) connecting to the daemon's Evaluator.
Allows discussing workflow needs, reviewing generated actuator proposals/diffs,
and confirming builds interactively.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := loadConfig(); err != nil {
			return err
		}

		fmt.Println("Clara Interactive Assistant REPL")
		fmt.Println("Type your workflow request, or 'exit'/'quit' to end session.")
		fmt.Println("---------------------------------------------------------")

		scanner := bufio.NewScanner(os.Stdin)
		for {
			fmt.Print("\nclara> ")
			if !scanner.Scan() {
				break
			}
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			if line == "exit" || line == "quit" {
				fmt.Println("Ending chat session.")
				break
			}

			resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
				Method: ipc.MethodChat,
				Params: map[string]any{"prompt": line},
			})
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}
			if resp.Error != "" {
				fmt.Printf("Error: %s\n", resp.Error)
				continue
			}

			// Render proposal decision
			data, _ := json.MarshalIndent(resp.Data, "", "  ")
			fmt.Printf("\nEvaluator Proposal:\n%s\n", string(data))
		}
		return nil
	},
}

func init() {
	approvalsCmd.AddCommand(approvalsListCmd, approvalsShowCmd, approvalsDecideCmd, approvalsSubmitCmd)
	rootCmd.AddCommand(approvalsCmd, requestCmd, chatCmd)
}
