package templ

import "strconv"

// itoa converts an int to a string for use inside templ expressions.
func itoa(n int) string {
	return strconv.Itoa(n)
}

// strVal extracts a string from a map[string]any value, returning "" if
// the value is absent or not a string.
func strVal(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
