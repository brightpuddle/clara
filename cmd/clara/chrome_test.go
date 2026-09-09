package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveSocketPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get user home dir: %v", err)
	}

	tests := []struct {
		name         string
		profile      string
		customSocket string
		expected     string
	}{
		{
			name:         "default no profile",
			profile:      "",
			customSocket: "",
			expected:     filepath.Join(home, ".local", "share", "clara", "chrome-bridge.sock"),
		},
		{
			name:         "with profile name",
			profile:      "work",
			customSocket: "",
			expected:     filepath.Join(home, ".local", "share", "clara", "chrome-work.sock"),
		},
		{
			name:         "explicit absolute custom socket",
			profile:      "work",
			customSocket: "/tmp/custom-chrome.sock",
			expected:     "/tmp/custom-chrome.sock",
		},
		{
			name:         "tilde path expansion",
			profile:      "",
			customSocket: "~/sockets/chrome.sock",
			expected:     filepath.Join(home, "sockets", "chrome.sock"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveSocketPath(tc.profile, tc.customSocket)
			if got != tc.expected {
				t.Errorf("resolveSocketPath(%q, %q) = %q; want %q", tc.profile, tc.customSocket, got, tc.expected)
			}
		})
	}
}

func TestUpdateExtensionWithProfile(t *testing.T) {
	tmpDir := t.TempDir()
	chromeProfileFlag = "work"
	defer func() {
		chromeProfileFlag = ""
	}()

	cmd := chromeUpdateExtCmd
	if err := cmd.RunE(cmd, []string{tmpDir}); err != nil {
		t.Fatalf("update-extension failed: %v", err)
	}

	bgPath := filepath.Join(tmpDir, "background.js")
	bgContent, err := os.ReadFile(bgPath)
	if err != nil {
		t.Fatalf("failed to read background.js: %v", err)
	}
	if !strings.Contains(string(bgContent), "com.brightpuddle.clara.work") {
		t.Errorf("background.js does not contain expected host name com.brightpuddle.clara.work: %s", string(bgContent))
	}

	manifestPath := filepath.Join(tmpDir, "manifest.json")
	manifestContent, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("failed to read manifest.json: %v", err)
	}
	if !strings.Contains(string(manifestContent), "Clara Browser Bridge (work)") {
		t.Errorf("manifest.json does not contain expected extension name: %s", string(manifestContent))
	}
}
