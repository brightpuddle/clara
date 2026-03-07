// Package watcher monitors filesystem directories for new/changed files.
package watcher

import (
	"context"
	"path/filepath"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
)

const debounceDelay = 300 * time.Millisecond

// Op represents a filesystem operation.
type Op uint32

const (
	OpCreate Op = iota
	OpWrite
	OpRemove
	OpRename
)

// FileEvent represents a filesystem change event.
type FileEvent struct {
	Path string
	Op   Op
}

// Watcher monitors directories recursively and emits debounced FileEvents.
type Watcher struct {
	fsw    *fsnotify.Watcher
	dirs   []string
	events chan FileEvent
	logger zerolog.Logger
}

// New creates a new directory Watcher.
func New(dirs []string, logger zerolog.Logger) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, errors.Wrap(err, "create fsnotify watcher")
	}
	return &Watcher{
		fsw:    fsw,
		dirs:   dirs,
		events: make(chan FileEvent, 256),
		logger: logger,
	}, nil
}

// Events returns the channel of debounced FileEvents.
func (w *Watcher) Events() <-chan FileEvent {
	return w.events
}

// Start begins watching all configured directories.
func (w *Watcher) Start(ctx context.Context) error {
	for _, dir := range w.dirs {
		expanded, err := filepath.Glob(dir)
		if err != nil || len(expanded) == 0 {
			expanded = []string{dir}
		}
		for _, d := range expanded {
			if err := w.fsw.Add(d); err != nil {
				w.logger.Warn().Err(err).Str("dir", d).Msg("failed to watch dir")
			} else {
				w.logger.Info().Str("dir", d).Msg("watching directory")
			}
		}
	}

	go w.run(ctx)
	return nil
}

// Close stops the watcher and closes the events channel.
func (w *Watcher) Close() error {
	return w.fsw.Close()
}

func (w *Watcher) run(ctx context.Context) {
	defer close(w.events)

	// Debounce: track pending events by path.
	pending := make(map[string]FileEvent)
	timer := time.NewTimer(debounceDelay)
	timer.Stop()

	flush := func() {
		for _, ev := range pending {
			select {
			case w.events <- ev:
			case <-ctx.Done():
				return
			}
		}
		pending = make(map[string]FileEvent)
	}

	for {
		select {
		case <-ctx.Done():
			flush()
			return

		case event, ok := <-w.fsw.Events:
			if !ok {
				flush()
				return
			}
			op := fsOpToOp(event.Op)
			pending[event.Name] = FileEvent{Path: event.Name, Op: op}
			timer.Reset(debounceDelay)

		case <-timer.C:
			flush()

		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			w.logger.Error().Err(err).Msg("fsnotify error")
		}
	}
}

func fsOpToOp(op fsnotify.Op) Op {
	switch {
	case op.Has(fsnotify.Create):
		return OpCreate
	case op.Has(fsnotify.Write):
		return OpWrite
	case op.Has(fsnotify.Remove):
		return OpRemove
	case op.Has(fsnotify.Rename):
		return OpRename
	default:
		return OpWrite
	}
}
