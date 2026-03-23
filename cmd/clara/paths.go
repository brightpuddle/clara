package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/brightpuddle/clara/internal/config"
	"github.com/spf13/cobra"
)

var pathsCmd = &cobra.Command{
	Use:   "paths",
	Short: "Show important Clara file and directory paths",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Config file:  %s\n", config.DefaultConfigPath())
		fmt.Printf("Data dir:     %s\n", cfg.DataDir)
		fmt.Printf("Socket:       %s\n", cfg.ControlSocketPath())
		fmt.Printf("Database:     %s\n", cfg.DBPath())
		fmt.Printf("Tasks dir:    %s\n", cfg.TasksDir())
		fmt.Printf("Log file:     %s\n", cfg.LogPath())

		// Try to find the extension directory if we're in a Homebrew-like setup
		exe, err := os.Executable()
		if err == nil {
			exe, _ = filepath.EvalSymlinks(exe)
			// If we are in /opt/homebrew/Cellar/clara/VERSION/bin/clara
			// then the extension is at /opt/homebrew/Cellar/clara/VERSION/extension
			base := filepath.Dir(filepath.Dir(exe))
			extPath := filepath.Join(base, "extension")
			if _, err := os.Stat(extPath); err == nil {
				fmt.Printf("Extension:    %s\n", extPath)
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(pathsCmd)
}
