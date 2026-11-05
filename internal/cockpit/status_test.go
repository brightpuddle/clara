package cockpit

import (
	"testing"
)

func TestFormatStatusShort(t *testing.T) {
	tests := []struct {
		name     string
		sessions []SessionSummary
		want     string
	}{
		{
			name:     "empty",
			sessions: nil,
			want:     "idle",
		},
		{
			name: "running only",
			sessions: []SessionSummary{
				{State: "running"},
				{State: "running"},
				{State: "closed"},
			},
			want: "2 running",
		},
		{
			name: "mixed active",
			sessions: []SessionSummary{
				{State: "running"},
				{State: "blocked"},
				{State: "idle"},
				{State: "closed"},
			},
			want: "1 running, 1 blocked, 1 idle",
		},
		{
			name: "idle only",
			sessions: []SessionSummary{
				{State: "idle"},
				{State: "idle"},
			},
			want: "2 idle",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatStatusShort(tc.sessions)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
