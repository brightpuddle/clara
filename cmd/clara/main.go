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
	"github.com/brightpuddle/clara/internal/tui"
	"github.com/spf13/cobra"
)

var (
	cfgFile string
	cfg     *config.Config
)

var rootCmd = &cobra.Command{
	Use:   "clara",
	Short: "Local agentic orchestrator for macOS",
	Long: `Clara is a local-first agentic orchestrator for macOS.

Run 'clara serve' to start the background agent.
Run 'clara agent status' to check on a running agent.
Run 'clara --help' to see all available commands.`,
	RunE:         runHUD,
	SilenceUsage: true,
}

func init() {
	rootCmd.PersistentFlags().StringVarP(
		&cfgFile, "config", "c", "", "path to config file",
	)
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		return loadConfig()
	}

	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(intentCmd)
	rootCmd.AddCommand(toolCmd)
	rootCmd.AddCommand(mcpCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func loadConfig() error {
	var err error
	if cfgFile != "" {
		cfg, err = config.Load(cfgFile)
	} else {
		cfg, err = config.LoadDefault()
	}
	return err
}

// runHUD is the default command: launch the interactive TUI.
func runHUD(cmd *cobra.Command, args []string) error {
	return tui.Run(cfg)
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

// sendRequest dials the agent control socket and sends a JSON-encoded request.
func sendRawRequest(socketPath string, req ipc.Request) (*ipc.Response, error) {
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck

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
