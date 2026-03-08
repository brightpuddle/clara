package ingestor

import (
	"testing"
)

func TestIsTextFile(t *testing.T) {
	textFiles := []string{
		"README.md",
		"notes.txt",
		"main.go",
		"script.py",
		"app.js",
		"types.ts",
		"config.yaml",
		"config.yml",
		"package.json",
		"Makefile.toml",
		"deploy.sh",
		".env",
		"output.log",
		"data.csv",
		"index.html",
		"data.xml",
	}
	for _, f := range textFiles {
		if !isTextFile(f) {
			t.Errorf("isTextFile(%q) = false, want true", f)
		}
	}
}

func TestIsTextFile_BinaryFiles(t *testing.T) {
	binFiles := []string{
		"photo.jpg",
		"image.png",
		"archive.zip",
		"binary.exe",
		"library.so",
		"font.ttf",
		"video.mp4",
		"audio.mp3",
		"document.pdf",
		"database.db",
	}
	for _, f := range binFiles {
		if isTextFile(f) {
			t.Errorf("isTextFile(%q) = true, want false", f)
		}
	}
}

func TestIsTextFile_CaseInsensitive(t *testing.T) {
	if !isTextFile("README.MD") {
		t.Error("isTextFile(README.MD) = false, want true (case insensitive)")
	}
	if !isTextFile("main.GO") {
		t.Error("isTextFile(main.GO) = false, want true (case insensitive)")
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input   string
		max     int
		want    string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello"},
		{"", 10, ""},
		{"abc", 3, "abc"},
		{"abcd", 3, "abc"},
		{"hello", 0, ""},
	}
	for _, tc := range tests {
		got := truncate(tc.input, tc.max)
		if got != tc.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.input, tc.max, got, tc.want)
		}
	}
}
