// Package theme manages the active bubbletint color theme for the Clara TUI.
// It supports dark, light, and system modes. When no theme is configured the
// caller receives nil, meaning "use native 16-color terminal". System mode
// polls the OS appearance via the native worker (no external tools needed).
package theme

import (
	bubbletint "github.com/lrstanley/bubbletint/v2"

	"github.com/brightpuddle/clara/internal/config"
)

// Manager resolves which bubbletint.Tint is currently active.
type Manager struct {
	mode      string // "dark", "light", "system"
	darkTint  *bubbletint.Tint
	lightTint *bubbletint.Tint
	isDark    bool
}

// New creates a Manager from the given TUI config.
// It returns nil Tints for themes left empty (native 16-color mode).
func New(cfg *config.TUIConfig) *Manager {
	m := &Manager{
		mode:   cfg.ThemeMode,
		isDark: true, // default to dark
	}
	if cfg.DarkTheme != "" && cfg.DarkTheme != "native" {
		m.darkTint = bubbletint.DefaultTintsByID(cfg.DarkTheme)
	}
	if cfg.LightTheme != "" && cfg.LightTheme != "native" {
		m.lightTint = bubbletint.DefaultTintsByID(cfg.LightTheme)
	}
	return m
}

// Current returns the active tint, or nil if native 16-color terminal should be used.
func (m *Manager) Current() *bubbletint.Tint {
	switch m.mode {
	case "light":
		return m.lightTint
	case "dark":
		return m.darkTint
	default: // "system"
		if m.isDark {
			return m.darkTint
		}
		return m.lightTint
	}
}

// SetDark updates whether the system is in dark mode.
// Called by the TUI when the periodic theme poll returns a new value.
func (m *Manager) SetDark(dark bool) {
	m.isDark = dark
}
