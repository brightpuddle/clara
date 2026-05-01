package main

import (
	"fmt"
	"strings"

	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/brightpuddle/clara/internal/theme"
	"github.com/spf13/cobra"
)

var pluginCmd = &cobra.Command{
	Use:   "plugin",
	Short: "Manage native Go plugins",
}

var pluginListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all available plugins",
	RunE:  runPluginList,
}

var pluginLoadCmd = &cobra.Command{
	Use:   "load <name>",
	Short: "Load a native plugin",
	Args:  cobra.ExactArgs(1),
	RunE:  runPluginLoad,
}

var pluginUnloadCmd = &cobra.Command{
	Use:   "unload <name>",
	Short: "Unload a native plugin",
	Args:  cobra.ExactArgs(1),
	RunE:  runPluginUnload,
}

var pluginReloadCmd = &cobra.Command{
	Use:   "reload <name>",
	Short: "Reload a native plugin",
	Args:  cobra.ExactArgs(1),
	RunE:  runPluginReload,
}

func init() {
	pluginCmd.AddCommand(
		pluginListCmd,
		pluginLoadCmd,
		pluginUnloadCmd,
		pluginReloadCmd,
	)
}

func runPluginList(cmd *cobra.Command, args []string) error {
	resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{Method: ipc.MethodPluginList})
	if err != nil {
		return err
	}

	if wantJSON() {
		prettyPrint(resp.Data)
		return nil
	}

	theme := theme.DetectTheme()
	plugins, ok := resp.Data.([]any)
	if !ok {
		return fmt.Errorf("unexpected response format")
	}

	if len(plugins) == 0 {
		fmt.Println(theme.Dimmed("No plugins found."))
		return nil
	}

	fmt.Println(theme.TitleStyle.Render("Native Plugins"))
	for _, p := range plugins {
		pMap, ok := p.(map[string]any)
		if !ok {
			continue
		}
		name, _ := pMap["name"].(string)
		status, _ := pMap["status"].(string)

		statusColor := theme.Dimmed
		if strings.EqualFold(status, "Loaded") {
			statusColor = theme.Green
		}

		fmt.Printf("  %-20s %s\n", theme.Cyan(name), statusColor(status))
	}

	return nil
}

func runPluginLoad(cmd *cobra.Command, args []string) error {
	name := args[0]
	resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
		Method: ipc.MethodPluginLoad,
		Params: map[string]any{"name": name},
	})
	if err != nil {
		return err
	}
	fmt.Println(resp.Message)
	return nil
}

func runPluginUnload(cmd *cobra.Command, args []string) error {
	name := args[0]
	resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
		Method: ipc.MethodPluginUnload,
		Params: map[string]any{"name": name},
	})
	if err != nil {
		return err
	}
	fmt.Println(resp.Message)
	return nil
}

func runPluginReload(cmd *cobra.Command, args []string) error {
	name := args[0]
	resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
		Method: ipc.MethodPluginReload,
		Params: map[string]any{"name": name},
	})
	if err != nil {
		return err
	}
	fmt.Println(resp.Message)
	return nil
}
