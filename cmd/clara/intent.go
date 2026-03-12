package main

import (
	"fmt"

	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/spf13/cobra"
)

var intentCmd = &cobra.Command{
	Use:   "intent",
	Short: "Manage intents",
}

var intentListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List all active intents",
	RunE:         runIntentList,
	SilenceUsage: true,
}

var intentRunCmd = &cobra.Command{
	Use:          "run <id>",
	Short:        "Manually trigger an intent by ID",
	Args:         cobra.ExactArgs(1),
	RunE:         runIntentRun,
	SilenceUsage: true,
}

func init() {
	intentCmd.AddCommand(intentListCmd, intentRunCmd)
}

func runIntentList(cmd *cobra.Command, args []string) error {
	resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{Method: ipc.MethodList})
	if err != nil {
		return fmt.Errorf("list request failed: %w", err)
	}
	prettyPrint(resp.Data)
	return nil
}

func runIntentRun(cmd *cobra.Command, args []string) error {
	resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
		Method: ipc.MethodRun,
		Params: map[string]any{"id": args[0]},
	})
	if err != nil {
		return fmt.Errorf("run request failed: %w", err)
	}
	fmt.Println(resp.Message)
	return nil
}
