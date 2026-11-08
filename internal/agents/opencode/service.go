// Package opencode is a peer client of the opencode background service
// (ADR 0008). It discovers and supervises the service, mirrors session
// activity into the Clara host log, and sends prompts and form replies back.
//
// It deliberately does not implement adapter.Adapter: sessions live in the
// service and are shared with other clients, so Clara neither spawns them nor
// owns their prompt queue.
package opencode

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/cockroachdb/errors"
)

var (
	// ErrNotRunning means no live service registration was found.
	ErrNotRunning = errors.New("opencode service is not running")
	// ErrResolved means another client answered first (or the form is gone).
	ErrResolved = errors.New("already resolved")
)

// Endpoint is how to reach the service. The password is regenerated on every
// service start, so never cache an Endpoint across a connection failure.
type Endpoint struct {
	URL      string
	Password string
	Version  string
}

// DefaultStateFile is where the service registers itself.
func DefaultStateFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "opencode", "service.json")
}

// Discover reads the service registration file and checks the service pid is
// alive. A missing file or dead pid yields ErrNotRunning.
func Discover(stateFile string) (Endpoint, error) {
	b, err := os.ReadFile(stateFile)
	if errors.Is(err, os.ErrNotExist) {
		return Endpoint{}, errors.Mark(err, ErrNotRunning)
	}
	if err != nil {
		return Endpoint{}, errors.Wrap(err, "read opencode service file")
	}
	var f struct {
		URL      string `json:"url"`
		Version  string `json:"version"`
		PID      int    `json:"pid"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return Endpoint{}, errors.Wrap(err, "parse opencode service file")
	}
	if f.URL == "" {
		return Endpoint{}, errors.New("opencode service file has no url")
	}
	if f.PID > 0 {
		if err := syscall.Kill(f.PID, 0); err != nil && !errors.Is(err, syscall.EPERM) {
			return Endpoint{}, errors.Mark(errors.Newf("service pid %d is dead", f.PID), ErrNotRunning)
		}
	}
	return Endpoint{URL: f.URL, Password: f.Password, Version: f.Version}, nil
}

// StartService runs `opencode service start`, the default Config.Start.
func StartService(ctx context.Context) error {
	out, err := exec.CommandContext(ctx, "opencode", "service", "start").CombinedOutput()
	if err != nil {
		return errors.Wrapf(err, "opencode service start: %s", out)
	}
	return nil
}
