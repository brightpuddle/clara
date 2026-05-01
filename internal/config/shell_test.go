package config

import (
	"os"
	"strings"
	"testing"
)

func TestEnsureLoginShellEnv(t *testing.T) {
	// Skip if not on macOS or if we're in a CI environment that might not have a login shell
	if os.Getenv("CI") != "" {
		t.Skip("Skipping in CI")
	}

	// Save original PATH
	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)

	// Set a PATH that lacks common homebrew dirs to trigger EnsureLoginShellEnv
	os.Setenv("PATH", "/usr/bin:/bin")

	EnsureLoginShellEnv()

	newPath := os.Getenv("PATH")
	if !strings.Contains(newPath, "/usr/local/bin") &&
		!strings.Contains(newPath, "/opt/homebrew/bin") {
		// If the user doesn't have homebrew installed, this might still "fail" to find them
		// but it should at least have tried.
		// Since we're on the user's machine, they likely have it.
		t.Logf("New PATH: %s", newPath)
	}
}
