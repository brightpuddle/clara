package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/brightpuddle/clara"
	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

var (
	chromeProfileFlag string
	chromeSocketFlag  string
)

var chromeCmd = &cobra.Command{
	Use:   "chrome",
	Short: "Manage the Clara Chrome extension",
}

func resolveSocketPath(profile, customSocket string) string {
	if customSocket != "" {
		if strings.HasPrefix(customSocket, "~/") {
			home, _ := os.UserHomeDir()
			return filepath.Join(home, customSocket[2:])
		}
		return customSocket
	}
	home, _ := os.UserHomeDir()
	if profile != "" {
		return filepath.Join(home, ".local", "share", "clara", fmt.Sprintf("chrome-%s.sock", profile))
	}
	return filepath.Join(home, ".local", "share", "clara", "chrome-bridge.sock")
}

var chromeNativeHostCmd = &cobra.Command{
	Use:    "native-host [profile]",
	Short:  "Run the Chrome Native Messaging host (internal use)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		profile := chromeProfileFlag
		if len(args) > 0 && profile == "" {
			profile = args[0]
		}
		socketPath := resolveSocketPath(profile, chromeSocketFlag)
		return runNativeHost(cmd.Context(), profile, socketPath)
	},
}

var chromeSetupNativeCmd = &cobra.Command{
	Use:   "setup-native <extension-id>",
	Short: "Install the Chrome Native Messaging host manifest",
	Long: `Install the Native Messaging host manifest so Chrome can launch Clara
as a native host.

Steps:
  1. Run:  clara chrome update-extension [--profile <profile>]
  2. Open Chrome → chrome://extensions  →  enable Developer mode
  3. Click "Load unpacked" and select the printed extension directory
  4. Copy the Extension ID shown on that page
  5. Run:  clara chrome setup-native <EXTENSION_ID> [--profile <profile>]
  6. Quit and relaunch Chrome`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		extID := args[0]
		profile := chromeProfileFlag
		socketPath := resolveSocketPath(profile, chromeSocketFlag)

		exe, err := os.Executable()
		if err != nil {
			return err
		}
		exe, _ = filepath.EvalSymlinks(exe)

		home, _ := os.UserHomeDir()
		dataDir := filepath.Join(home, ".local", "share", "clara")
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			return err
		}

		wrapperName := "clara-chrome-native-host"
		hostName := "com.brightpuddle.clara"
		nativeArgs := "chrome native-host"
		if profile != "" {
			wrapperName = fmt.Sprintf("clara-chrome-native-host-%s", profile)
			hostName = fmt.Sprintf("com.brightpuddle.clara.%s", profile)
			nativeArgs = fmt.Sprintf("chrome native-host --socket %q", socketPath)
		} else if chromeSocketFlag != "" {
			nativeArgs = fmt.Sprintf("chrome native-host --socket %q", socketPath)
		}

		wrapperPath := filepath.Join(dataDir, wrapperName)
		wrapperScript := fmt.Sprintf("#!/bin/sh\nexec %q %s\n", exe, nativeArgs)
		if err := os.WriteFile(wrapperPath, []byte(wrapperScript), 0755); err != nil {
			return err
		}

		manifest := map[string]any{
			"name":            hostName,
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

		destPath := filepath.Join(destDir, hostName+".json")
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
  ~/.local/share/clara/extension/ (or ~/.local/share/clara/extension-<profile>/ if --profile is set)

Pass an explicit path to override.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		profile := chromeProfileFlag
		target := ""
		if len(args) > 0 {
			target = args[0]
		} else if profile != "" {
			home, _ := os.UserHomeDir()
			target = filepath.Join(home, ".local", "share", "clara", fmt.Sprintf("extension-%s", profile))
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
				srcData, err := clara.ExtensionFS.ReadFile(path)
				if err != nil {
					return err
				}
				if profile != "" {
					if rel == "background.js" {
						hostName := fmt.Sprintf("com.brightpuddle.clara.%s", profile)
						srcData = []byte(strings.ReplaceAll(string(srcData), "com.brightpuddle.clara", hostName))
					} else if rel == "manifest.json" {
						extName := fmt.Sprintf("Clara Browser Bridge (%s)", profile)
						srcData = []byte(strings.ReplaceAll(string(srcData), "Clara Browser Bridge", extName))
					}
				}
				return os.WriteFile(dest, srcData, 0644)
			},
		)
		if err != nil {
			return err
		}

		fmt.Printf("✓ Extension files written to:\n  %s\n\n", target)
		fmt.Println("Next: load this directory in Chrome as an unpacked extension,")
		if profile != "" {
			fmt.Printf("then run:  clara chrome setup-native <EXTENSION_ID> --profile %s\n", profile)
		} else {
			fmt.Println("then run:  clara chrome setup-native <EXTENSION_ID>")
		}
		return nil
	},
}

func init() {
	chromeNativeHostCmd.Flags().StringVarP(&chromeProfileFlag, "profile", "p", "", "Chrome profile identifier")
	chromeNativeHostCmd.Flags().StringVarP(&chromeSocketFlag, "socket", "s", "", "Bridge Unix domain socket path")

	chromeSetupNativeCmd.Flags().StringVarP(&chromeProfileFlag, "profile", "p", "", "Chrome profile identifier")
	chromeSetupNativeCmd.Flags().StringVarP(&chromeSocketFlag, "socket", "s", "", "Bridge Unix domain socket path")

	chromeUpdateExtCmd.Flags().StringVarP(&chromeProfileFlag, "profile", "p", "", "Chrome profile identifier")

	chromeCmd.AddCommand(chromeSetupNativeCmd)
	chromeCmd.AddCommand(chromeUpdateExtCmd)
	chromeCmd.AddCommand(chromeNativeHostCmd)
	rootCmd.AddCommand(chromeCmd)
}

func runNativeHost(ctx context.Context, profile, udsPath string) error {
	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".local", "share", "clara")

	// Debug log — written to a file so we can inspect it even when Chrome
	// captures stderr. Truncated on each fresh launch.
	logFile := "chrome-native-host.log"
	if profile != "" {
		logFile = fmt.Sprintf("chrome-native-host-%s.log", profile)
	}
	debugPath := filepath.Join(dataDir, logFile)
	debugFile, _ := os.OpenFile(debugPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	dlog := zerolog.New(zerolog.MultiLevelWriter(os.Stderr, debugFile)).
		With().Timestamp().Logger()

	dlog.Info().Str("profile", profile).Str("uds", udsPath).Msg("native host started")

	// 1. Connect to UDS
	var conn net.Conn
	var err error
	for i := 0; i < 5; i++ {
		conn, err = net.Dial("unix", udsPath)
		if err == nil {
			break
		}
		dlog.Warn().Err(err).Int("attempt", i+1).Msg("dial UDS failed, retrying")
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		dlog.Error().
			Err(err).
			Msg("could not connect to bridge UDS — is the chrome integration loaded?")
		return errors.Wrap(err, "dial chrome bridge UDS")
	}
	dlog.Info().Msg("connected to bridge UDS")
	defer conn.Close()

	errCh := make(chan error, 2)

	// 2. Chrome (stdin) -> UDS
	go func() {
		for {
			var length uint32
			if err := binary.Read(os.Stdin, binary.LittleEndian, &length); err != nil {
				dlog.Info().Err(err).Msg("stdin closed")
				errCh <- err
				return
			}
			msg := make([]byte, length)
			if _, err := io.ReadFull(os.Stdin, msg); err != nil {
				dlog.Error().Err(err).Msg("stdin read body failed")
				errCh <- err
				return
			}
			dlog.Debug().Int("bytes", len(msg)).Msg("chrome→uds")
			if _, err := conn.Write(append(msg, '\n')); err != nil {
				dlog.Error().Err(err).Msg("UDS write failed")
				errCh <- err
				return
			}
		}
	}()

	// 3. UDS -> Chrome (stdout)
	go func() {
		decoder := json.NewDecoder(conn)
		for {
			var msg json.RawMessage
			if err := decoder.Decode(&msg); err != nil {
				dlog.Info().Err(err).Msg("UDS closed")
				errCh <- err
				return
			}
			dlog.Debug().Int("bytes", len(msg)).Msg("uds→chrome")
			if err := binary.Write(os.Stdout, binary.LittleEndian, uint32(len(msg))); err != nil {
				dlog.Error().Err(err).Msg("stdout write length failed")
				errCh <- err
				return
			}
			if _, err := os.Stdout.Write(msg); err != nil {
				dlog.Error().Err(err).Msg("stdout write body failed")
				errCh <- err
				return
			}
		}
	}()

	select {
	case <-ctx.Done():
		dlog.Info().Msg("context cancelled")
		return nil
	case err := <-errCh:
		if errors.Is(err, io.EOF) {
			dlog.Info().Msg("EOF — clean disconnect")
			return nil
		}
		dlog.Error().Err(err).Msg("native host exiting with error")
		return err
	}
}
