package orchestrator_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/brightpuddle/clara/internal/orchestrator"
)

func TestRemindersTaskwarriorSyncIntentParses(t *testing.T) {
	path, data := mustReadPoCIntentFile(t, filepath.Join("..", "..", "tmp"))
	intent, err := orchestrator.ParseIntent(data)
	if err != nil {
		t.Fatalf("ParseIntent(%q): %v", path, err)
	}
	if intent.ID != "reminders-taskwarrior-sync" {
		t.Fatalf("unexpected intent ID %q", intent.ID)
	}
}

func mustReadPoCIntentFile(t *testing.T, baseDir string) (string, []byte) {
	t.Helper()

	for _, name := range []string{
		"reminders-taskwarrior-sync.yaml",
		"reminders-taskwarrior-sync.yml",
		"reminders-taskwarrior-sync.json",
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

	t.Fatalf("no reminders-taskwarrior-sync intent fixture found in %q", baseDir)
	return "", nil
}
