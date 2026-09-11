package trigger_test

import (
	"testing"

	"github.com/brightpuddle/clara/internal/trigger"
)

func TestRule_Match(t *testing.T) {
	sampleEvent := map[string]any{
		"id":     "ev-12345",
		"type":   "email.received",
		"source": "sensor.mail",
		"data": map[string]any{
			"mailbox":  "ops@brightpuddle.com",
			"subject":  "[CRITICAL] Outage in EU-WEST-1",
			"priority": 1,
			"tags":     []any{"infra", "alert", "prod"},
			"headers": map[string]any{
				"from": "pagerduty@brightpuddle.com",
			},
		},
	}

	tests := []struct {
		name     string
		rule     trigger.Rule
		data     any
		expected bool
	}{
		{
			name: "Simple equality on top level type",
			rule: trigger.Rule{
				Field: "type",
				Op:    trigger.OpEquals,
				Value: "email.received",
			},
			data:     sampleEvent,
			expected: true,
		},
		{
			name: "Nested dot notation field equals",
			rule: trigger.Rule{
				Field: "data.mailbox",
				Op:    trigger.OpEquals,
				Value: "ops@brightpuddle.com",
			},
			data:     sampleEvent,
			expected: true,
		},
		{
			name: "Nested deep dot notation",
			rule: trigger.Rule{
				Field: "data.headers.from",
				Op:    trigger.OpEquals,
				Value: "pagerduty@brightpuddle.com",
			},
			data:     sampleEvent,
			expected: true,
		},
		{
			name: "Contains in string",
			rule: trigger.Rule{
				Field: "data.subject",
				Op:    trigger.OpContains,
				Value: "Outage",
			},
			data:     sampleEvent,
			expected: true,
		},
		{
			name: "Regex match on subject",
			rule: trigger.Rule{
				Field: "data.subject",
				Op:    trigger.OpRegex,
				Value: "(?i)outage in eu-west-1",
			},
			data:     sampleEvent,
			expected: true,
		},
		{
			name: "In slice (tags)",
			rule: trigger.Rule{
				Field: "data.tags",
				Op:    trigger.OpContains,
				Value: "alert",
			},
			data:     sampleEvent,
			expected: true,
		},
		{
			name: "Numeric comparison (priority <= 2)",
			rule: trigger.Rule{
				Field: "data.priority",
				Op:    trigger.OpLte,
				Value: 2,
			},
			data:     sampleEvent,
			expected: true,
		},
		{
			name: "Numeric comparison failure (priority > 5)",
			rule: trigger.Rule{
				Field: "data.priority",
				Op:    trigger.OpGt,
				Value: 5,
			},
			data:     sampleEvent,
			expected: false,
		},
		{
			name: "Compound AND rule: email.received AND ops mailbox AND (critical subject OR high priority)",
			rule: trigger.Rule{
				And: []*trigger.Rule{
					{
						Field: "type",
						Op:    trigger.OpEquals,
						Value: "email.received",
					},
					{
						Field: "data.mailbox",
						Op:    trigger.OpEquals,
						Value: "ops@brightpuddle.com",
					},
					{
						Or: []*trigger.Rule{
							{
								Field: "data.subject",
								Op:    trigger.OpContains,
								Value: "CRITICAL",
							},
							{
								Field: "data.priority",
								Op:    trigger.OpEquals,
								Value: 0,
							},
						},
					},
				},
			},
			data:     sampleEvent,
			expected: true,
		},
		{
			name: "NOT operator: NOT dev mailbox",
			rule: trigger.Rule{
				Not: &trigger.Rule{
					Field: "data.mailbox",
					Op:    trigger.OpEquals,
					Value: "dev@brightpuddle.com",
				},
			},
			data:     sampleEvent,
			expected: true,
		},
		{
			name: "Exists operator on existing header",
			rule: trigger.Rule{
				Field: "data.headers.from",
				Op:    trigger.OpExists,
			},
			data:     sampleEvent,
			expected: true,
		},
		{
			name: "Exists operator on non-existing header",
			rule: trigger.Rule{
				Field: "data.headers.reply_to",
				Op:    trigger.OpExists,
			},
			data:     sampleEvent,
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			matched, err := tc.rule.Match(tc.data)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if matched != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, matched)
			}
		})
	}
}
