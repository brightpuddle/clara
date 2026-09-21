package config

import "testing"

func TestMCPExposeProfile_Allows(t *testing.T) {
	cases := []struct {
		name    string
		profile MCPExposeProfile
		tool    string
		want    bool
	}{
		{
			name:    "empty include denies everything",
			profile: MCPExposeProfile{},
			tool:    "mail.search",
			want:    false,
		},
		{
			name:    "namespace glob matches tool in namespace",
			profile: MCPExposeProfile{Include: []string{"mail.*"}},
			tool:    "mail.search",
			want:    true,
		},
		{
			name:    "namespace glob does not match a different namespace with shared prefix",
			profile: MCPExposeProfile{Include: []string{"mail.*"}},
			tool:    "mailbox.search",
			want:    false,
		},
		{
			name:    "exact name matches",
			profile: MCPExposeProfile{Include: []string{"github.create_issue"}},
			tool:    "github.create_issue",
			want:    true,
		},
		{
			name:    "exact name does not match sibling tool",
			profile: MCPExposeProfile{Include: []string{"github.create_issue"}},
			tool:    "github.close_issue",
			want:    false,
		},
		{
			name: "exclude carves out a tool from an included namespace",
			profile: MCPExposeProfile{
				Include: []string{"fs.*"},
				Exclude: []string{"fs.delete_file"},
			},
			tool: "fs.delete_file",
			want: false,
		},
		{
			name: "exclude does not affect other tools in the namespace",
			profile: MCPExposeProfile{
				Include: []string{"fs.*"},
				Exclude: []string{"fs.delete_file"},
			},
			tool: "fs.read_file",
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.profile.Allows(tc.tool); got != tc.want {
				t.Errorf("Allows(%q) = %v, want %v", tc.tool, got, tc.want)
			}
		})
	}
}
