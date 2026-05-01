package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/brightpuddle/clara/internal/theme"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Manage MCP servers",
}

var mcpListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all MCP servers",
	RunE:  runMCPList,
}

var mcpStartCmd = &cobra.Command{
	Use:   "start <name>",
	Short: "Start a managed MCP server",
	Args:  cobra.ExactArgs(1),
	RunE:  runMCPStart,
}

var mcpStopCmd = &cobra.Command{
	Use:   "stop <name>",
	Short: "Stop a managed MCP server",
	Args:  cobra.ExactArgs(1),
	RunE:  runMCPStop,
}

var mcpRestartCmd = &cobra.Command{
	Use:   "restart <name>",
	Short: "Restart a managed MCP server",
	Args:  cobra.ExactArgs(1),
	RunE:  runMCPRestart,
}

var (
	mcpAddURL         string
	mcpAddDescription string
	mcpAddToken       string
	mcpAddSkipVerify  bool
	mcpAddEnv         []string
)

var mcpAddCmd = &cobra.Command{
	Use:   "add <name> [command] [args...]",
	Short: "Add a new managed MCP server to the configuration",
	Long: `Add a new managed MCP server.
If adding a command-based server:
  clara mcp add my-server my-command --arg1 val1
If adding an HTTP-based server:
  clara mcp add my-server --url http://localhost:8080`,
	Args: cobra.MinimumNArgs(1),
	RunE: runMCPAdd,
}

var mcpRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a managed MCP server from the configuration",
	Args:  cobra.ExactArgs(1),
	RunE:  runMCPRemove,
}

func init() {
	mcpAddCmd.Flags().StringVar(&mcpAddURL, "url", "", "HTTP URL for the MCP server")
	mcpAddCmd.Flags().StringVar(&mcpAddDescription, "description", "", "Description of the MCP server")
	mcpAddCmd.Flags().StringVar(&mcpAddToken, "token", "", "Bearer token for HTTP MCP server")
	mcpAddCmd.Flags().BoolVar(&mcpAddSkipVerify, "skip-verify", false, "Skip TLS verification for HTTP MCP server")
	mcpAddCmd.Flags().StringSliceVarP(&mcpAddEnv, "env", "e", nil, "Environment variables in KEY=VALUE format")

	mcpCmd.AddCommand(
		mcpListCmd,
		mcpStartCmd,
		mcpStopCmd,
		mcpRestartCmd,
		mcpAddCmd,
		mcpRemoveCmd,
	)
}

func runMCPList(cmd *cobra.Command, args []string) error {
	resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{Method: ipc.MethodMCPList})
	if err != nil {
		return err
	}

	if wantJSON() {
		prettyPrint(resp.Data)
		return nil
	}

	theme := theme.DetectTheme()
	data, ok := resp.Data.(map[string]any)
	if !ok {
		return fmt.Errorf("unexpected response format")
	}

	managed, _ := data["managed"].([]any)
	active, _ := data["active"].([]any)
	pending, _ := data["pending"].([]any)

	if len(managed) > 0 {
		fmt.Println(theme.TitleStyle.Render("Managed Servers (configured in config.yaml)"))
		for _, m := range managed {
			srv := m.(map[string]any)
			name := srv["name"].(string)
			status := srv["status"].(string)

			statusColor := theme.Dimmed
			switch strings.ToUpper(status) {
			case "RUNNING":
				statusColor = theme.Green
			case "FAILED":
				statusColor = theme.Red
			case "CONNECTING":
				statusColor = theme.Yellow
			}

			fmt.Printf("  %-20s %s\n", theme.Cyan(name), statusColor(status))
		}
		fmt.Println()
	}

	if len(active) > 0 {
		fmt.Println(theme.TitleStyle.Render("Active Servers (dynamically registered)"))
		for _, a := range active {
			fmt.Printf("  %s\n", theme.Cyan(a.(string)))
		}
		fmt.Println()
	}

	if len(pending) > 0 {
		fmt.Println(theme.TitleStyle.Render("Pending Connections (waiting for server to connect)"))
		for _, p := range pending {
			fmt.Printf("  %s\n", theme.Yellow(p.(string)))
		}
		fmt.Println()
	}

	if len(managed) == 0 && len(active) == 0 && len(pending) == 0 {
		fmt.Println(theme.Dimmed("No MCP servers found."))
	}

	return nil
}

func runMCPStart(cmd *cobra.Command, args []string) error {
	name := args[0]
	resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
		Method: ipc.MethodMCPStart,
		Params: map[string]any{"name": name},
	})
	if err != nil {
		return err
	}
	fmt.Println(resp.Message)
	return nil
}

func runMCPStop(cmd *cobra.Command, args []string) error {
	name := args[0]
	resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
		Method: ipc.MethodMCPStop,
		Params: map[string]any{"name": name},
	})
	if err != nil {
		return err
	}
	fmt.Println(resp.Message)
	return nil
}

func runMCPRestart(cmd *cobra.Command, args []string) error {
	name := args[0]
	resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
		Method: ipc.MethodMCPRestart,
		Params: map[string]any{"name": name},
	})
	if err != nil {
		return err
	}
	fmt.Println(resp.Message)
	return nil
}

func runMCPAdd(cmd *cobra.Command, args []string) error {
	name := args[0]
	var command string
	var commandArgs []string

	if len(args) > 1 {
		command = args[1]
		commandArgs = args[2:]
	}

	if command == "" && mcpAddURL == "" {
		return fmt.Errorf("either a command or --url is required")
	}

	envMap := make(map[string]string)
	for _, env := range mcpAddEnv {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	fullCommand := command
	if strings.ContainsAny(fullCommand, " \t\n\"'\\") {
		fullCommand = fmt.Sprintf("%q", fullCommand)
	}
	for _, arg := range commandArgs {
		if strings.ContainsAny(arg, " \t\n\"'\\") {
			fullCommand += fmt.Sprintf(" %q", arg)
		} else {
			fullCommand += " " + arg
		}
	}

	params := map[string]any{
		"name":        name,
		"command":     fullCommand,
		"url":         mcpAddURL,
		"description": mcpAddDescription,
		"token":       mcpAddToken,
		"skip_verify": mcpAddSkipVerify,
		"overwrite":   false,
	}
	if len(envMap) > 0 {
		params["env"] = envMap
	}

	resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
		Method: ipc.MethodMCPAdd,
		Params: params,
	})

	if err != nil && strings.Contains(err.Error(), "already exists") {
		// Prompt for overwrite
		if isTerminalFile(os.Stdin) {
			fmt.Printf("Server %q already exists. Overwrite? [y/N] ", name)
			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				ans := strings.ToLower(strings.TrimSpace(scanner.Text()))
				if ans == "y" || ans == "yes" {
					params["overwrite"] = true
					resp, err = sendRequest(cfg.ControlSocketPath(), ipc.Request{
						Method: ipc.MethodMCPAdd,
						Params: params,
					})
				} else {
					return fmt.Errorf("aborted")
				}
			}
		} else {
			return err
		}
	}

	if err != nil {
		return err
	}

	fmt.Println(resp.Message)
	return nil
}

func runMCPRemove(cmd *cobra.Command, args []string) error {
	name := args[0]
	resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
		Method: ipc.MethodMCPRemove,
		Params: map[string]any{"name": name},
	})
	if err != nil {
		return err
	}
	fmt.Println(resp.Message)
	return nil
}
