package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sub := filepath.Join(home, "src", "clara")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(home, "file.txt")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "tilde subdir", in: "~/src/clara", want: sub},
		{name: "bare tilde", in: "~", want: home},
		{name: "absolute", in: sub, want: sub},
		{name: "trailing slash cleaned", in: sub + "/", want: sub},
		{name: "relative", in: "src/clara", wantErr: true},
		{name: "missing", in: "~/nope", wantErr: true},
		{name: "not a directory", in: "~/file.txt", wantErr: true},
		{name: "tilde user unsupported", in: "~other/x", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveDir(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveDir(%q) = %q, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveDir(%q) error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("resolveDir(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
