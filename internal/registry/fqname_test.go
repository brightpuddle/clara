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
		{"clara-db", "db.search", "clara-db.db.search"},
		{"macos", "reminders_list", "macos.reminders_list"},
		{"macos", "mail_search", "macos.mail_search"},
		{"clara-search", "mail.search", "clara-search.mail.search"},
		{"tmux", "session.list", "tmux.session.list"},
		{"task", "pending.list", "task.pending.list"},
	}
	
	for _, tc := range cases {
		got := r.GetFQToolName(tc.server, tc.tool)
		if got != tc.want {
			t.Errorf("GetFQToolName(%q, %q) = %q, want %q", tc.server, tc.tool, got, tc.want)
		}
	}
}
