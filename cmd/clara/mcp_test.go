package main

import (
	"path/filepath"
	"testing"

	"github.com/brightpuddle/clara/internal/config"
)

func TestResolveMCPDBPath(t *testing.T) {
	if got := resolveMCPDBPath(""); got != "" {
		t.Fatalf("empty path = %q, want empty", got)
	}

	if got := resolveMCPDBPath(":memory:"); got != ":memory:" {
		t.Fatalf("memory path = %q", got)
	}

	abs := "/tmp/clara.db"
	if got := resolveMCPDBPath(abs); got != abs {
		t.Fatalf("absolute path = %q, want %q", got, abs)
	}

	want := filepath.Join(config.DefaultDataDir(), "data.db")
	if got := resolveMCPDBPath("data.db"); got != want {
		t.Fatalf("relative path = %q, want %q", got, want)
	}
}
