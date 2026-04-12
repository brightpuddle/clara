package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsTerminalFileReturnsFalseForRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stderr.log")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer file.Close()

	if isTerminalFile(file) {
		t.Fatal("expected regular file to not be detected as a terminal")
	}
}
