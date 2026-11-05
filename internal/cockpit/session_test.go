package cockpit

import (
	"os"
	"testing"
)

func TestResolveSession(t *testing.T) {
	sessions := []SessionSummary{
		{ID: "01M6MV02V85TRXGQFRH7S6R8YE", Host: "eve", Agent: "agy", Title: "Model Selection", State: "idle", Dir: "/Users/nathan/src/clara/main"},
		{ID: "01M6M19F2ZHS111EGPNX9546K4", Host: "eve", Agent: "opencode", Title: "Smoke Test", State: "closed", Dir: "/tmp/smoke"},
		{ID: "01M6ABCXYZ1234567890ABCDEF", Host: "sophia", Agent: "opencode", Title: "Lucia Feature", State: "running", Dir: "/Users/nathan/src/lucia/main"},
	}

	tests := []struct {
		name    string
		query   string
		wantID  string
		wantErr bool
	}{
		{name: "exact full ID", query: "01M6MV02V85TRXGQFRH7S6R8YE", wantID: "01M6MV02V85TRXGQFRH7S6R8YE"},
		{name: "prefix match", query: "01M6MV", wantID: "01M6MV02V85TRXGQFRH7S6R8YE"},
		{name: "case insensitive suffix or prefix", query: "s6r8ye", wantID: "01M6MV02V85TRXGQFRH7S6R8YE"},
		{name: "title match", query: "Model Selection", wantID: "01M6MV02V85TRXGQFRH7S6R8YE"},
		{name: "title partial match", query: "lucia", wantID: "01M6ABCXYZ1234567890ABCDEF"},
		{name: "not found", query: "nonexistent", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveSession(sessions, tc.query)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for query %q, got nil", tc.query)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.ID != tc.wantID {
				t.Fatalf("got ID %q, want %q", got.ID, tc.wantID)
			}
		})
	}
}

func TestGitStatus(t *testing.T) {
	// Test on the current working directory which is a git repo
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	info := DetectGitStatus(cwd)
	if !info.IsRepo {
		t.Fatalf("expected %q to be recognized as git repo", cwd)
	}
	if info.Branch == "" {
		t.Fatalf("expected non-empty branch")
	}

	// Test non-git dir
	tmp := t.TempDir()
	nonGit := DetectGitStatus(tmp)
	if nonGit.IsRepo {
		t.Fatalf("expected %q to NOT be recognized as git repo", tmp)
	}
}
