// Package intentlog provides append-only structured logging for intent runs.
// Each intent gets its own JSONL file under a shared logs directory, written
// via a lumberjack rolling writer so files are bounded in size automatically.
//
// Files are named <intentID>.log and each line is a JSON object:
//
//	{"time":"...","run_id":"...","intent_id":"...","entrypoint":"...",
//	 "state":"...","action":"...","args":{...},"result":{...},"error":"..."}
package intentlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Event is one structured log line emitted during intent execution.
type Event struct {
	Time       time.Time `json:"time"`
	RunID      string    `json:"run_id"`
	IntentID   string    `json:"intent_id"`
	Entrypoint string    `json:"entrypoint,omitempty"`
	State      string    `json:"state,omitempty"`
	Action     string    `json:"action,omitempty"`
	Args       any       `json:"args,omitempty"`
	Result     any       `json:"result,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// Logger writes intent events to per-intent JSONL log files.
type Logger struct {
	dir     string
	mu      sync.Mutex
	writers map[string]*lumberjack.Logger
}

// New creates a Logger that writes files into dir (created if absent).
func New(dir string) (*Logger, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, errors.Wrap(err, "create intent logs dir")
	}
	return &Logger{
		dir:     dir,
		writers: make(map[string]*lumberjack.Logger),
	}, nil
}

// Append writes one event as a JSONL line to the intent's log file.
func (l *Logger) Append(event Event) error {
	if event.Time.IsZero() {
		event.Time = time.Now()
	}

	line, err := json.Marshal(event)
	if err != nil {
		return errors.Wrap(err, "marshal intent event")
	}

	l.mu.Lock()
	w := l.writerFor(event.IntentID)
	_, err = fmt.Fprintf(w, "%s\n", line)
	l.mu.Unlock()

	return errors.Wrap(err, "write intent event")
}

// Close flushes and closes all open log writers.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	var first error
	for _, w := range l.writers {
		if err := w.Close(); err != nil && first == nil {
			first = err
		}
	}
	l.writers = make(map[string]*lumberjack.Logger)
	return errors.Wrap(first, "close intent log writers")
}

// writerFor returns (or creates) the lumberjack writer for the given intentID.
// Caller must hold l.mu.
func (l *Logger) writerFor(intentID string) *lumberjack.Logger {
	if w, ok := l.writers[intentID]; ok {
		return w
	}
	w := &lumberjack.Logger{
		Filename:   l.FilePath(intentID),
		MaxSize:    10,
		MaxBackups: 5,
		MaxAge:     30,
		Compress:   false,
	}
	l.writers[intentID] = w
	return w
}

// FilePath returns the log file path for the given intentID.
func (l *Logger) FilePath(intentID string) string {
	return filepath.Join(l.dir, intentID+".log")
}

// Dir returns the directory used by this logger.
func (l *Logger) Dir() string {
	return l.dir
}
