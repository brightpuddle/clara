package providers

import (
	"reflect"
	"testing"
)

func TestDecodeJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected any
	}{
		{
			name:     "Empty input",
			input:    "",
			expected: nil,
		},
		{
			name:     "Whitespace input",
			input:    "   \n\t  ",
			expected: nil,
		},
		{
			name:  "Simple object",
			input: `{"key": "value"}`,
			expected: map[string]any{
				"key": "value",
			},
		},
		{
			name:  "Simple array",
			input: `["one", "two"]`,
			expected: []any{
				"one",
				"two",
			},
		},
		{
			name:  "Markdown object",
			input: "```json\n{\"key\": \"value\"}\n```",
			expected: map[string]any{
				"key": "value",
			},
		},
		{
			name:  "Markdown object without json tag",
			input: "```\n{\"key\": \"value\"}\n```",
			expected: map[string]any{
				"key": "value",
			},
		},
		{
			name:  "Markdown object with text before/after",
			input: "Here is your JSON:\n```json\n{\"key\": \"value\"}\n```\nHope it helps!",
			expected: map[string]any{
				"key": "value",
			},
		},
		{
			name:  "Markdown array",
			input: "```json\n[\"one\", \"two\"]\n```",
			expected: []any{
				"one",
				"two",
			},
		},
		{
			name:  "Markdown without newline",
			input: "```json{\"key\": \"value\"}```",
			expected: map[string]any{
				"key": "value",
			},
		},
		{
			name:  "Single backticks",
			input: "`{\"key\": \"value\"}`",
			expected: map[string]any{
				"key": "value",
			},
		},
		{
			name:     "Invalid JSON",
			input:    `{"key": "value"`,
			expected: nil,
		},
		{
			name:     "Non-JSON text",
			input:    "This is just some text",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecodeJSON(tt.input)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("DecodeJSON() = %v, want %v", got, tt.expected)
			}
		})
	}
}
