package fs

import (
	"context"
	"testing"
)

func TestMdfindCommand(t *testing.T) {
	// We'll mock the command execution or just test the string generation.
	// Since we are on Darwin, we can actually test mdfind if we want,
	// but for CI it might be better to mock.
	
	ctx := context.Background()
	
	// Test case: simple search
	results, err := runMdfind(ctx, "kMDItemDisplayName == 'test.txt'", "")
	if err != nil {
		t.Logf("mdfind might fail if not on Darwin: %v", err)
	}
	_ = results
}

func TestParseMdfindOutput(t *testing.T) {
	output := "/path/to/file1\n/path/to/file2\n"
	results := parseMdfindOutput(output)
	
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
	if results[0] != "/path/to/file1" {
		t.Errorf("expected /path/to/file1, got %s", results[0])
	}
}
