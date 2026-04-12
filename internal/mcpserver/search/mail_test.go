package search

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMailTraversal(t *testing.T) {
	// Mock some mail files
	tmpDir := t.TempDir()
	mailDir := filepath.Join(tmpDir, "Mail")
	_ = os.MkdirAll(mailDir, 0o755)

	_ = os.WriteFile(
		filepath.Join(mailDir, "test1.eml"),
		[]byte("Subject: Test 1\n\nBody 1"),
		0o644,
	)
	_ = os.MkdirAll(filepath.Join(mailDir, "Inbox.mbox"), 0o755)
	_ = os.WriteFile(
		filepath.Join(mailDir, "Inbox.mbox", "test2.eml"),
		[]byte("Subject: Test 2\n\nBody 2"),
		0o644,
	)

	var files []string
	err := filepath.Walk(mailDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && filepath.Ext(path) == ".eml" {
			files = append(files, path)
		}
		return nil
	})

	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Errorf("expected 2 eml files, got %d", len(files))
	}
}

func TestParseEmail(t *testing.T) {
	tmpDir := t.TempDir()
	emlPath := filepath.Join(tmpDir, "test.eml")
	content := "Subject: Test Subject\nFrom: alice@example.com\nTo: bob@example.com\nDate: Mon, 2 Apr 2026 10:00:00 +0000\nMessage-ID: <123@example.com>\n\nHello Bob!"
	_ = os.WriteFile(emlPath, []byte(content), 0o644)

	doc, err := parseEmail(emlPath)
	if err != nil {
		t.Fatalf("parseEmail: %v", err)
	}

	if doc.Data["subject"] != "Test Subject" {
		t.Errorf("expected Test Subject, got %s", doc.Data["subject"])
	}
	if doc.Data["from"] != "alice@example.com" {
		t.Errorf("expected alice@example.com, got %s", doc.Data["from"])
	}
	if doc.Data["message_id"] != "<123@example.com>" {
		t.Errorf("expected <123@example.com>, got %s", doc.Data["message_id"])
	}
	if !strings.Contains(doc.Data["body"], "Hello Bob!") {
		t.Errorf("expected body to contain Hello Bob!, got %s", doc.Data["body"])
	}
}
