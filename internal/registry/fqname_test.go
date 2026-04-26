package registry

import (
	"testing"
)

func TestGetFQToolName(t *testing.T) {
	r := &Registry{}
	
	cases := []struct {
		server string
		tool   string
		want   string
	}{
		{"clara-db", "query", "clara-db.query"},
		{"clara-db", "db.search", "db.search"},
		{"macos", "reminders_list", "reminders.list"},
		{"macos", "mail_search", "mail.search"},
		{"clara-search", "mail.search", "mail.search"},
	}
	
	for _, tc := range cases {
		got := r.getFQToolName(tc.server, tc.tool)
		if got != tc.want {
			t.Errorf("getFQToolName(%q, %q) = %q, want %q", tc.server, tc.tool, got, tc.want)
		}
	}
}
