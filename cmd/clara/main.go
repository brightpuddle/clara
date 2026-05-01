// Package main is the unified Clara binary entry point.
// It provides both the background agent (via 'clara serve') and all CLI
// commands for managing intents, tools, and built-in MCP servers.
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/brightpuddle/clara/internal/config"
	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/spf13/cobra"
)

var (
	cfgFile   string
	cfg       *config.Config
	outputFmt string // "json" or "" (auto)
)

var rootCmd = &cobra.Command{
	Use:   "clara",
	Short: "Local agentic orchestrator for macOS",
	Long: `Clara is a local-first agentic orchestrator for macOS.

Run 'clara serve' to start the background agent.
Run 'clara status' to check on a running agent.
Run 'clara --help' to see all available commands.`,
	RunE:         runHUD,
	SilenceUsage: true,
}

// statusCmd mirrors 'clara agent status' at the top level for quick access.
var statusCmd = &cobra.Command{
	Use:          "status",
	Short:        "Show agent status (alias for 'clara agent status')",
	RunE:         runAgentStatus,
	SilenceUsage: true,
}

// runCmd mirrors 'clara intent run' at the top level for quick access.
var runCmd = &cobra.Command{
	Use:          "run <intent-file>",
	Short:        "Execute an intent file (alias for 'clara intent run')",
	Args:         cobra.ExactArgs(1),
	RunE:         runIntentRun,
	SilenceUsage: true,
}

func init() {
	rootCmd.PersistentFlags().StringVarP(
		&cfgFile, "config", "c", "", "path to config file",
	)
	rootCmd.PersistentFlags().StringVarP(
		&outputFmt, "output", "o", "", `output format: "" (auto) or "json"`,
	)
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		return loadConfig()
	}

	// Mirror the verbose flag onto the top-level run command.
	runCmd.Flags().BoolVarP(&intentRunVerbose, "verbose", "v", false, "show full tool args/results")

	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(intentCmd)
	rootCmd.AddCommand(toolCmd)
	rootCmd.AddCommand(pluginCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(runCmd)

}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func loadConfig() error {
	config.EnsureLoginShellEnv()
	var err error
	if cfgFile != "" {
		cfg, err = config.Load(cfgFile)
	} else {
		cfg, err = config.LoadDefault()
	}
	return err
}

// runHUD is the default command. The interactive HUD has been removed in
// Phase 4; this now prints a brief usage hint instead.
func runHUD(cmd *cobra.Command, _ []string) error {
	_ = cmd.Help()
	return nil
}

// wantJSON returns true when output should be machine-readable JSON:
// either the caller passed -o json, or stdout is not a terminal.
func wantJSON() bool {
	if outputFmt == "json" {
		return true
	}
	return !isTerminalFile(os.Stdout)
}

// ── Shared helpers ────────────────────────────────────────────────────────────

// isRunning returns true if the agent control socket is reachable.
func isRunning(socketPath string) bool {
	conn, err := net.DialTimeout("unix", socketPath, 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// sendRawRequest dials the agent control socket and sends a JSON-encoded request.
func sendRawRequest(socketPath string, req ipc.Request) (*ipc.Response, error) {
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	// Tool calls can take a long time (e.g. browser automation, slow LLMs).
	deadline := 10 * time.Minute
	if req.Method != ipc.MethodToolCall {
		deadline = 10 * time.Second
	}
	conn.SetDeadline(time.Now().Add(deadline)) //nolint:errcheck

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	var resp ipc.Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &resp, nil
}

// sendRequest dials the agent control socket and sends a JSON-encoded request.
func sendRequest(socketPath string, req ipc.Request) (*ipc.Response, error) {
	resp, err := sendRawRequest(socketPath, req)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return resp, fmt.Errorf("agent error: %s", resp.Error)
	}
	return resp, nil
}

func prettyPrint(v any) {
	if v == nil {
		return
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(v) //nolint:errcheck
}
