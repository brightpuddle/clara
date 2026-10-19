package config

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

var ensureShellEnvOnce sync.Once

// EnsureLoginShellEnv attempts to ensure that the process environment includes
// variables from the user's login shell (like PATH), which are often missing
// when running as a macOS LaunchAgent.
func EnsureLoginShellEnv() {
	ensureShellEnvOnce.Do(func() {
		ensureLoginShellEnv()
	})
}

func ensureLoginShellEnv() {
	// Only needed on Unix-like systems (especially macOS) where launchd or services
	// do not initialize user shell profile environments.
	if runtime.GOOS == "windows" {
		return
	}

	path := os.Getenv("PATH")

	// Collect well-known paths that should be in PATH if they exist on disk.
	var candidatePaths []string
	if runtime.GOOS == "darwin" {
		candidatePaths = append(
			candidatePaths,
			"/opt/homebrew/bin",
			"/opt/homebrew/sbin",
			"/usr/local/bin",
		)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidatePaths = append(candidatePaths,
			filepath.Join(home, ".bun", "bin"),
			filepath.Join(home, ".cargo", "bin"),
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, "go", "bin"),
		)
	}

	// Determine if we need to query the login shell.
	// On macOS / LaunchAgent environments, PATH is often just "/usr/bin:/bin:/usr/sbin:/sbin" (or with "/usr/local/bin").
	// If any existing candidate path on disk (e.g. /opt/homebrew/bin or ~/.bun/bin) is missing from PATH,
	// we definitely need to load the login shell environment.
	needsShellEnv := false
	for _, dir := range candidatePaths {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			if !containsPathEntry(path, dir) {
				needsShellEnv = true
				break
			}
		}
	}

	var shellPathVal string
	if needsShellEnv {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/zsh"
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		// Use -l to trigger a login shell (reads .zprofile, .zshenv, etc.)
		cmd := exec.CommandContext(ctx, shell, "-l", "-c", "env")
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil {
			scanner := bufio.NewScanner(&out)
			for scanner.Scan() {
				line := scanner.Text()
				parts := strings.SplitN(line, "=", 2)
				if len(parts) != 2 {
					continue
				}
				key := parts[0]
				val := parts[1]

				if key == "PATH" {
					shellPathVal = val
				} else if os.Getenv(key) == "" {
					os.Setenv(key, val)
				}
			}
		}
	}

	// Combine and deduplicate PATH entries:
	// 1. Existing PATH
	// 2. Shell PATH (from login shell)
	// 3. Any existing candidate paths on disk
	var mergedEntries []string
	if shellPathVal != "" {
		mergedEntries = append(mergedEntries, filepath.SplitList(shellPathVal)...)
	}
	mergedEntries = append(mergedEntries, filepath.SplitList(os.Getenv("PATH"))...)
	for _, dir := range candidatePaths {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			mergedEntries = append(mergedEntries, dir)
		}
	}

	finalPath := dedupePaths(mergedEntries)
	if len(finalPath) > 0 {
		os.Setenv("PATH", strings.Join(finalPath, string(os.PathListSeparator)))
	}
}

func containsPathEntry(pathList, target string) bool {
	for _, entry := range filepath.SplitList(pathList) {
		if filepath.Clean(entry) == filepath.Clean(target) {
			return true
		}
	}
	return false
}

func dedupePaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	var result []string
	for _, p := range paths {
		cleaned := filepath.Clean(strings.TrimSpace(p))
		if cleaned == "" || cleaned == "." {
			continue
		}
		if _, ok := seen[cleaned]; !ok {
			seen[cleaned] = struct{}{}
			result = append(result, cleaned)
		}
	}
	return result
}
