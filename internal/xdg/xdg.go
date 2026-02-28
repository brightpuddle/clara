package xdg

import (
	"os"
	"path/filepath"
)

const appName = "clara"

// ConfigDir returns the path to the clara config directory:
//
//	$XDG_CONFIG_HOME/clara  (defaults to ~/.config/clara)
//
// The directory is created if it does not already exist.
func ConfigDir() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	dir := filepath.Join(base, appName)
	return dir, os.MkdirAll(dir, 0o755)
}

// ConfigFile returns the full path to a named config file inside the clara
// config directory, creating the directory if necessary.
func ConfigFile(name string) (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}
