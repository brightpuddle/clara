package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
)

const maxCommandHistory = 200

type commandHistory struct {
	path  string
	limit int
	items []string
	index int
	draft string
}

func loadCommandHistory(path string, limit int) (*commandHistory, error) {
	h := &commandHistory{
		path:  path,
		limit: limit,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			h.resetNavigation()
			return h, nil
		}
		return nil, errors.Wrap(err, "read TUI history")
	}

	if len(strings.TrimSpace(string(data))) == 0 {
		h.resetNavigation()
		return h, nil
	}

	if err := json.Unmarshal(data, &h.items); err != nil {
		return nil, errors.Wrap(err, "decode TUI history")
	}
	h.trimToLimit()
	h.resetNavigation()
	return h, nil
}

func (h *commandHistory) Add(entry string) error {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return nil
	}

	if len(h.items) == 0 || h.items[len(h.items)-1] != entry {
		h.items = append(h.items, entry)
		h.trimToLimit()
	}
	h.resetNavigation()
	return h.save()
}

func (h *commandHistory) Previous(current string) string {
	if len(h.items) == 0 {
		return current
	}
	if h.index >= len(h.items) {
		h.index = len(h.items)
		h.draft = current
	}
	if h.index > 0 {
		h.index--
	}
	return h.items[h.index]
}

func (h *commandHistory) Next() string {
	if len(h.items) == 0 {
		return ""
	}
	if h.index < len(h.items)-1 {
		h.index++
		return h.items[h.index]
	}
	h.index = len(h.items)
	return h.draft
}

func (h *commandHistory) resetNavigation() {
	h.index = len(h.items)
	h.draft = ""
}

func (h *commandHistory) save() error {
	if err := os.MkdirAll(filepath.Dir(h.path), 0o755); err != nil {
		return errors.Wrap(err, "create TUI history directory")
	}
	data, err := json.Marshal(h.items)
	if err != nil {
		return errors.Wrap(err, "encode TUI history")
	}
	if err := os.WriteFile(h.path, data, 0o644); err != nil {
		return errors.Wrap(err, "write TUI history")
	}
	return nil
}

func (h *commandHistory) trimToLimit() {
	if h.limit <= 0 || len(h.items) <= h.limit {
		return
	}
	h.items = append([]string(nil), h.items[len(h.items)-h.limit:]...)
}
