// Package pathutil provides shared path helpers for Clara's built-in integrations.
package pathutil

import (
	"os"
	"path/filepath"
	"strings"
)

// Resolve expands environment variables and the leading "~" in path,
// returning the cleaned absolute-style path.
func Resolve(path string) string {
	path = os.ExpandEnv(strings.TrimSpace(path))
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
