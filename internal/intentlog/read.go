package intentlog

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/cockroachdb/errors"
)

// Filter constrains which events are returned by read functions.
type Filter struct {
	// RunID restricts to a specific run. Empty means all runs.
	RunID string
	// Entrypoint restricts to a specific task entrypoint. Empty means all.
	Entrypoint string
	// Since discards events older than this time. Zero means include all.
	Since time.Time
}

func (f Filter) Matches(e Event) bool {
	if f.RunID != "" && e.RunID != f.RunID {
		return false
	}
	if f.Entrypoint != "" && e.Entrypoint != f.Entrypoint {
		return false
	}
	if !f.Since.IsZero() && !e.Time.After(f.Since) {
		return false
	}
	return true
}

// ReadEvents reads events from a single intent log file applying filter.
// If n > 0, only the last n matching events are returned (tail mode).
// Events are returned in chronological order.
func ReadEvents(path string, filter Filter, n int) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errors.Wrapf(err, "open intent log %q", path)
	}
	defer f.Close()

	if n > 0 {
		return readTail(f, filter, n)
	}
	return readAll(f, filter)
}

// MergeEvents reads events across all *.log files in dir, merges by time,
// and applies filter. If n > 0, returns the last n matching events overall.
func MergeEvents(dir string, filter Filter, n int) ([]Event, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.log"))
	if err != nil {
		return nil, errors.Wrap(err, "glob intent logs")
	}

	var all []Event
	for _, path := range paths {
		events, err := ReadEvents(path, filter, 0)
		if err != nil {
			return nil, err
		}
		all = append(all, events...)
	}

	// Stable sort by time.
	sort.SliceStable(all, func(i, j int) bool {
		return all[i].Time.Before(all[j].Time)
	})

	if n > 0 && len(all) > n {
		all = all[len(all)-n:]
	}
	return all, nil
}

// ClearEvents truncates intent log files.
// If intentID is non-empty, only that intent's file is truncated.
// If intentID is empty, all *.log files in dir are truncated.
func ClearEvents(dir string, intentID string) error {
	if intentID != "" {
		path := filepath.Join(dir, intentID+".log")
		if err := os.Truncate(path, 0); err != nil && !os.IsNotExist(err) {
			return errors.Wrapf(err, "truncate intent log %q", path)
		}
		return nil
	}

	paths, err := filepath.Glob(filepath.Join(dir, "*.log"))
	if err != nil {
		return errors.Wrap(err, "glob intent logs")
	}
	for _, path := range paths {
		if err := os.Truncate(path, 0); err != nil && !os.IsNotExist(err) {
			return errors.Wrapf(err, "truncate intent log %q", path)
		}
	}
	return nil
}

// readAll scans a file from start, returning all matching events in order.
func readAll(f *os.File, filter Filter) ([]Event, error) {
	var events []Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var e Event
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue // skip malformed lines
		}
		if filter.Matches(e) {
			events = append(events, e)
		}
	}
	return events, errors.Wrap(scanner.Err(), "scan intent log")
}

// readTail scans a file and returns the last n matching events in order.
// Uses a circular buffer to avoid loading the entire file into memory.
func readTail(f *os.File, filter Filter, n int) ([]Event, error) {
	buf := make([]Event, n)
	start, count := 0, 0

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var e Event
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue
		}
		if !filter.Matches(e) {
			continue
		}
		buf[start] = e
		start = (start + 1) % n
		if count < n {
			count++
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, errors.Wrap(err, "scan intent log tail")
	}

	// Reconstruct in chronological order from the circular buffer.
	result := make([]Event, count)
	oldest := (start - count + n) % n
	for i := range count {
		result[i] = buf[(oldest+i)%n]
	}
	return result, nil
}
