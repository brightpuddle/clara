// Package theme manages the active bubbletint color theme for the Clara TUI.
// It supports dark, light, and system (dark-notify) modes. When no theme is
// configured the caller receives nil, which means "use native 16-color terminal".
package theme

import (
	"bufio"
	"context"
	"os/exec"
	"strings"

	bubbletint "github.com/lrstanley/bubbletint/v2"

	"github.com/brightpuddle/clara/internal/config"
)

// Manager resolves which bubbletint.Tint is currently active and watches for
// macOS appearance changes via dark-notify.
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

// SetDark updates whether the system is in dark mode (used by WatchDarkNotify).
func (m *Manager) SetDark(dark bool) {
	m.isDark = dark
}

// DarkNotifyInstalled reports whether dark-notify is available in PATH.
func DarkNotifyInstalled() bool {
	_, err := exec.LookPath("dark-notify")
	return err == nil
}

// WatchDarkNotify launches dark-notify and sends true (dark) or false (light) on
// the returned channel whenever macOS appearance changes. The goroutine exits when
// ctx is cancelled. The channel is never closed — callers should select on ctx.Done().
// Returns nil if dark-notify is not installed.
func WatchDarkNotify(ctx context.Context) <-chan bool {
	if !DarkNotifyInstalled() {
		return nil
	}

	ch := make(chan bool, 4)

	go func() {
		cmd := exec.CommandContext(ctx, "dark-notify")
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return
		}
		if err := cmd.Start(); err != nil {
			return
		}

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			switch line {
			case "dark":
				select {
				case ch <- true:
				case <-ctx.Done():
					return
				}
			case "light":
				select {
				case ch <- false:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return ch
}
