package config

import (
	"path"
)

// MatchAny reports whether the given name matches any of the provided patterns.
// Patterns support '*' as a wildcard. Matching is case-sensitive.
func MatchAny(name string, patterns []string) bool {
	for _, p := range patterns {
		if p == "*" {
			return true
		}
		// path.Match handles '*' and '?' and character classes.
		// It's convenient for our needs.
		matched, err := path.Match(p, name)
		if err == nil && matched {
			return true
		}
	}
	return false
}

// FilterExposed filters the list of names based on include patterns.
// If includePatterns is empty, no names are exposed (default deny).
func FilterExposed(names []string, includePatterns []string) []string {
	if len(includePatterns) == 0 {
		return nil
	}
	var filtered []string
	for _, name := range names {
		if MatchAny(name, includePatterns) {
			filtered = append(filtered, name)
		}
	}
	return filtered
}
