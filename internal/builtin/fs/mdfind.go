// mdfind.go provides macOS Spotlight search integration via the mdfind command.
package fs

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// runMdfind executes the mdfind command with a query and an optional onlyin directory.
func runMdfind(ctx context.Context, query string, onlyin string) ([]string, error) {
	args := []string{query}
	if onlyin != "" {
		args = append(args, "-onlyin", onlyin)
	}

	cmd := exec.CommandContext(ctx, "mdfind", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("mdfind: %w (output: %s)", err, string(output))
	}

	return parseMdfindOutput(string(output)), nil
}

// parseMdfindOutput splits the mdfind output by newlines and cleans empty entries.
func parseMdfindOutput(output string) []string {
	lines := strings.Split(output, "\n")
	var results []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			results = append(results, trimmed)
		}
	}
	return results
}
