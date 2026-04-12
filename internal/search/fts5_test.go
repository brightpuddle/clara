package search

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/ncruces/go-sqlite3/embed"
)

func TestFTS5Indexing(t *testing.T) {
	ctx := context.Background()
	tmpDir, err := os.MkdirTemp("", "clara-search-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	
	// Create a new indexer
	// We want to index "documents" which have an ID, title, and body.
	// We'll use an external content table strategy.
	
	schema := &IndexSchema{
		Name: "docs",
		Columns: []string{"title", "body"},
	}
	
	indexer, err := NewIndexer(dbPath, schema)
	if err != nil {
		t.Fatalf("failed to create indexer: %v", err)
	}
	defer indexer.Close()

	// Test case: Indexing a document
	doc := &Document{
		ID: "doc1",
		Data: map[string]string{
			"title": "Hello World",
			"body":  "This is a test document about SQLite and FTS5.",
		},
	}
	
	if err := indexer.Index(ctx, doc); err != nil {
		t.Fatalf("failed to index document: %v", err)
	}

	// Test case: Searching
	results, err := indexer.Search(ctx, "SQLite", 0)
	if err != nil {
		t.Fatalf("failed to search: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	} else if results[0].ID != "doc1" {
		t.Errorf("expected doc1, got %s", results[0].ID)
	}
}

func TestIndexerReservedKeyword(t *testing.T) {
	ctx := context.Background()
	tmpDir, err := os.MkdirTemp("", "fts5-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	schema := &IndexSchema{
		Name:    "test",
		Columns: []string{"subject", "from", "to", "body"},
	}

	indexer, err := NewIndexer(dbPath, schema)
	if err != nil {
		t.Fatalf("failed to create indexer: %v", err)
	}
	defer indexer.Close()

	doc := &Document{
		ID: "doc1",
		Data: map[string]string{
			"subject": "hello",
			"from":    "alice@example.com",
			"to":      "bob@example.com",
			"body":    "how are you?",
		},
	}

	if err := indexer.Index(ctx, doc); err != nil {
		t.Fatalf("failed to index document: %v", err)
	}

	results, err := indexer.Search(ctx, "hello", 0)
	if err != nil {
		t.Fatalf("failed to search: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	} else if results[0].ID != "doc1" {
		t.Errorf("expected ID doc1, got %s", results[0].ID)
	}
}
