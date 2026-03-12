package main

import (
	"fmt"

	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage the Clara agent lifecycle",
}

var agentStartCmd = &cobra.Command{
	Use:          "start",
	Short:        "Start the Clara agent",
	RunE:         runAgentStart,
	SilenceUsage: true,
}

var agentStopCmd = &cobra.Command{
	Use:          "stop",
	Short:        "Stop the running Clara agent",
	RunE:         runAgentStop,
	SilenceUsage: true,
}

var agentStatusCmd = &cobra.Command{
	Use:          "status",
	Short:        "Show agent status and active intents",
	RunE:         runAgentStatus,
	SilenceUsage: true,
}

func init() {
	agentCmd.AddCommand(agentStartCmd, agentStopCmd, agentStatusCmd)
}

func runAgentStart(cmd *cobra.Command, args []string) error {
	if isRunning(cfg.ControlSocketPath()) {
		fmt.Println("Clara agent is already running.")
		return nil
	}
	fmt.Println("Start the agent with: clara serve")
	return nil
}

func runAgentStop(cmd *cobra.Command, args []string) error {
	resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{Method: ipc.MethodShutdown})
	if err != nil {
		return fmt.Errorf("agent not reachable: %w", err)
	}
	fmt.Println(resp.Message)
	return nil
}

func runAgentStatus(cmd *cobra.Command, args []string) error {
	if !isRunning(cfg.ControlSocketPath()) {
		fmt.Println("Clara agent is not running.")
		return nil
	}
	resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{Method: ipc.MethodStatus})
	if err != nil {
		return fmt.Errorf("status request failed: %w", err)
	}
	prettyPrint(resp.Data)
	return nil
}
