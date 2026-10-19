package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEnsureLoginShellEnv(t *testing.T) {
	if os.Getenv("CI") != "" || runtime.GOOS != "darwin" {
		t.Skip("Skipping in CI or non-darwin")
	}

	// Save original PATH
	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)

	// Set a PATH that has /usr/local/bin (standard macOS /etc/paths) but lacks /opt/homebrew/bin
	os.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin:/usr/local/bin")

	ensureLoginShellEnv()

	newPath := os.Getenv("PATH")
	if fi, err := os.Stat("/opt/homebrew/bin"); err == nil && fi.IsDir() {
		if !containsPathEntry(newPath, "/opt/homebrew/bin") {
			t.Errorf("Expected /opt/homebrew/bin to be in PATH, got: %s", newPath)
		}
	}
}

func TestDedupePaths(t *testing.T) {
	input := []string{"/usr/bin", "/bin", "/usr/bin", "/opt/homebrew/bin", "", "."}
	result := dedupePaths(input)
	expected := []string{"/usr/bin", "/bin", "/opt/homebrew/bin"}

	if len(result) != len(expected) {
		t.Fatalf("expected len %d, got %d (%v)", len(expected), len(result), result)
	}
	for i, v := range expected {
		if result[i] != filepath.Clean(v) {
			t.Errorf("at index %d: expected %s, got %s", i, v, result[i])
		}
	}
}
