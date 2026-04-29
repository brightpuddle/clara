package config

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// EnsureLoginShellEnv attempts to ensure that the process environment includes
// variables from the user's login shell (like PATH), which are often missing
// when running as a macOS LaunchAgent.
func EnsureLoginShellEnv() {
	// Only apply this on macOS when we detect we're in a restricted environment.
	// A good signal is if /opt/homebrew/bin or /usr/local/bin are missing from PATH
	// on a system where they likely should exist.
	path := os.Getenv("PATH")
	if strings.Contains(path, "/opt/homebrew/bin") || strings.Contains(path, "/usr/local/bin") {
		return
	}

	// Try to resolve the environment from the user's shell.
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh" // Default to zsh on modern macOS
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Use -l to trigger a login shell (reads .zprofile, .zshenv, etc.)
	cmd := exec.CommandContext(ctx, shell, "-l", "-c", "env")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return
	}

	scanner := bufio.NewScanner(&out)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		val := parts[1]

		// We primarily care about PATH, but might as well pick up others
		// like HOME, USER, etc. if they were somehow missing.
		if key == "PATH" {
			os.Setenv(key, val)
		}
	}
}
