package orchestrator_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/brightpuddle/clara/internal/orchestrator"
)

func TestRemindersTaskwarriorSyncIntentParses(t *testing.T) {
	path, data := mustReadPoCIntentFile(t, filepath.Join("..", "..", "tmp"))
	intent, err := orchestrator.LoadIntentFile(path, data, nil)
	if err != nil {
		t.Fatalf("LoadIntentFile(%q): %v", path, err)
	}
	if intent.ID != "reminders-taskwarrior-sync" {
		t.Fatalf("unexpected intent ID %q", intent.ID)
	}
}

func mustReadPoCIntentFile(t *testing.T, baseDir string) (string, []byte) {
	t.Helper()

	for _, name := range []string{
		"reminders_taskwarrior_sync.star",
		"reminders-taskwarrior-sync.star",
	} {
		path := filepath.Join(baseDir, name)
		data, err := os.ReadFile(path)
		if err == nil {
			return path, data
		}
		if !os.IsNotExist(err) {
			t.Fatalf("ReadFile(%q): %v", path, err)
		}
	}

	t.Skipf("no reminders-taskwarrior-sync .star intent fixture found in %q", baseDir)
	return "", nil
}
