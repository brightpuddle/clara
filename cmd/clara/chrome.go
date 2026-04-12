package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/brightpuddle/clara"
	"github.com/spf13/cobra"
)

var chromeCmd = &cobra.Command{
	Use:   "chrome",
	Short: "Manage the Clara Chrome extension",
}

var chromeSetupNativeCmd = &cobra.Command{
	Use:   "setup-native <extension-id>",
	Short: "Install the Chrome Native Messaging host manifest",
	Long: `Install the Native Messaging host manifest so Chrome can launch Clara
as a native host.

Steps:
  1. Run:  clara chrome update-extension
  2. Open Chrome → chrome://extensions  →  enable Developer mode
  3. Click "Load unpacked" and select the printed extension directory
  4. Copy the Extension ID shown on that page
  5. Run:  clara chrome setup-native <EXTENSION_ID>
  6. Quit and relaunch Chrome`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		extID := args[0]
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		exe, _ = filepath.EvalSymlinks(exe)

		// Chrome Native Messaging does not support an "args" field in the
		// manifest. The "path" must be a standalone executable with no
		// additional arguments. We write a small wrapper script that execs
		// the correct clara subcommand and point the manifest at that.
		home, _ := os.UserHomeDir()
		dataDir := filepath.Join(home, ".local", "share", "clara")
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			return err
		}
		wrapperPath := filepath.Join(dataDir, "clara-chrome-native-host")
		wrapperScript := fmt.Sprintf("#!/bin/sh\nexec %q mcpserver chrome chrome-native-host\n", exe)
		if err := os.WriteFile(wrapperPath, []byte(wrapperScript), 0755); err != nil {
			return err
		}

		manifest := map[string]any{
			"name":            "com.brightpuddle.clara",
			"description":     "Clara Browser Bridge",
			"path":            wrapperPath,
			"type":            "stdio",
			"allowed_origins": []string{"chrome-extension://" + extID + "/"},
		}

		manifestJSON, _ := json.MarshalIndent(manifest, "", "  ")
		destDir := filepath.Join(
			home,
			"Library",
			"Application Support",
			"Google",
			"Chrome",
			"NativeMessagingHosts",
		)
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return err
		}

		destPath := filepath.Join(destDir, "com.brightpuddle.clara.json")
		if err := os.WriteFile(destPath, manifestJSON, 0644); err != nil {
			return err
		}

		fmt.Printf("✓ Wrapper script written to:\n  %s\n\n", wrapperPath)
		fmt.Printf("✓ Native Messaging manifest installed:\n  %s\n\n", destPath)
		fmt.Println("Next: quit and relaunch Chrome. The extension icon should turn green.")
		return nil
	},
}

// defaultExtensionDir returns the canonical path for the unpacked extension.
func defaultExtensionDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "clara", "extension")
}

var chromeUpdateExtCmd = &cobra.Command{
	Use:   "update-extension [path]",
	Short: "Update the extension files on disk from the embedded versions",
	Long: `Write the latest extension files (embedded in the clara binary) to disk
so Chrome can load them as an unpacked extension.

The default destination is:
  ~/.local/share/clara/extension/

Pass an explicit path to override.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target := ""
		if len(args) > 0 {
			target = args[0]
		} else {
			target = defaultExtensionDir()
		}

		if err := os.MkdirAll(target, 0755); err != nil {
			return err
		}

		err := fs.WalkDir(
			clara.ExtensionFS,
			"extension",
			func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				rel, err := filepath.Rel("extension", path)
				if err != nil {
					return err
				}
				if rel == "." {
					return nil
				}
				dest := filepath.Join(target, rel)
				if d.IsDir() {
					return os.MkdirAll(dest, 0755)
				}
				srcFile, err := clara.ExtensionFS.Open(path)
				if err != nil {
					return err
				}
				defer srcFile.Close()
				destFile, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
				if err != nil {
					return err
				}
				defer destFile.Close()
				_, err = io.Copy(destFile, srcFile)
				return err
			},
		)
		if err != nil {
			return err
		}

		fmt.Printf("✓ Extension files written to:\n  %s\n\n", target)
		fmt.Println("Next: load this directory in Chrome as an unpacked extension,")
		fmt.Println("then run:  clara chrome setup-native <EXTENSION_ID>")
		return nil
	},
}

func init() {
	chromeCmd.AddCommand(chromeSetupNativeCmd)
	chromeCmd.AddCommand(chromeUpdateExtCmd)
	rootCmd.AddCommand(chromeCmd)
}
