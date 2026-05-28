// Package ringbuf provides a thread-safe fixed-capacity ring buffer for
// streaming JSON log entries from the daemon to CLI subscribers.
package ringbuf

import (
	"context"
	"encoding/json"
	"sync"
)

// RingBuffer is a thread-safe fixed-capacity circular buffer of JSON entries.
// Old entries are overwritten when the buffer is full (FIFO eviction).
type RingBuffer struct {
	mu      sync.RWMutex
	entries []json.RawMessage
	cap     int
	head    int // index of the oldest entry
	size    int // number of valid entries

	subsMu sync.Mutex
	subs   []chan json.RawMessage
}

// New creates a new RingBuffer with the given capacity.
func New(cap int) *RingBuffer {
	return &RingBuffer{
		entries: make([]json.RawMessage, cap),
		cap:     cap,
	}
}

// Push appends a JSON entry, evicting the oldest if the buffer is full,
// and fans out to all active subscribers.
func (r *RingBuffer) Push(entry json.RawMessage) {
	r.mu.Lock()
	if r.size < r.cap {
		idx := (r.head + r.size) % r.cap
		r.entries[idx] = entry
		r.size++
	} else {
		// Overwrite the oldest slot and advance head.
		r.entries[r.head] = entry
		r.head = (r.head + 1) % r.cap
	}
	r.mu.Unlock()

	// Fan out to subscribers (non-blocking).
	r.subsMu.Lock()
	for _, ch := range r.subs {
		select {
		case ch <- entry:
		default:
			// Slow subscriber — drop rather than block.
		}
	}
	r.subsMu.Unlock()
}

// Snapshot returns up to tail historical entries (oldest first) without
// subscribing to future entries. Use this for non-follow reads.
func (r *RingBuffer) Snapshot(tail int) []json.RawMessage {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := r.size
	if tail >= 0 && tail < count {
		count = tail
	}
	out := make([]json.RawMessage, count)
	startOffset := r.size - count
	for i := 0; i < count; i++ {
		idx := (r.head + startOffset + i) % r.cap
		out[i] = r.entries[idx]
	}
	return out
}

// Subscribe returns a channel that first receives up to tail historical entries
// (oldest first) and then receives new entries as they arrive.
// The channel is closed when ctx is cancelled.
func (r *RingBuffer) Subscribe(ctx context.Context, tail int) <-chan json.RawMessage {
	ch := make(chan json.RawMessage, 256)

	r.mu.RLock()
	// Collect up to tail historical entries.
	count := r.size
	if tail >= 0 && tail < count {
		count = tail
	}
	history := make([]json.RawMessage, count)
	for i := 0; i < count; i++ {
		// Walk backwards: most recent is at head+size-1, oldest at head.
		// We want the last `count` entries ordered oldest→newest.
		startOffset := r.size - count
		idx := (r.head + startOffset + i) % r.cap
		history[i] = r.entries[idx]
	}
	r.mu.RUnlock()

	// Register subscriber before sending history so we don't miss events.
	r.subsMu.Lock()
	r.subs = append(r.subs, ch)
	r.subsMu.Unlock()

	go func() {
		defer close(ch)
		// Drain history first.
		for _, e := range history {
			select {
			case ch <- e:
			case <-ctx.Done():
				r.removeSub(ch)
				return
			}
		}
		// Block until context is cancelled; new entries arrive via Push fan-out.
		<-ctx.Done()
		r.removeSub(ch)
	}()

	return ch
}

func (r *RingBuffer) removeSub(ch chan json.RawMessage) {
	r.subsMu.Lock()
	defer r.subsMu.Unlock()
	for i, s := range r.subs {
		if s == ch {
			r.subs = append(r.subs[:i], r.subs[i+1:]...)
			return
		}
	}
}
