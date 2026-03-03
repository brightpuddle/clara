package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/brightpuddle/clara/internal/xdg"
	"gopkg.in/yaml.v3"
)

// localConfig holds the subset of agent config relevant to the TUI.
type localConfig struct {
	NotesDir string
}

// readLocalConfig reads notes dir from agent config, with env var and default fallbacks.
// Priority: CLARA_NOTES_DIR env → agent.yaml notes.dir → ~/notes
func readLocalConfig() localConfig {
	// Env override takes highest priority.
	if v := os.Getenv("CLARA_NOTES_DIR"); v != "" {
		return localConfig{NotesDir: expandHome(v)}
	}

	// Try to parse agent.yaml.
	if path, err := xdg.ConfigFile("agent.yaml"); err == nil {
		if data, err := os.ReadFile(path); err == nil {
			var raw struct {
				Notes struct {
					Dir string `yaml:"dir"`
				} `yaml:"notes"`
			}
			if err := yaml.Unmarshal(data, &raw); err == nil && raw.Notes.Dir != "" {
				return localConfig{NotesDir: expandHome(raw.Notes.Dir)}
			}
		}
	}

	// Default.
	home, _ := os.UserHomeDir()
	return localConfig{NotesDir: filepath.Join(home, "notes")}
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}
