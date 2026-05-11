package webexapi

import (
	"encoding/json"
	"sync"
	"time"
)

// Event is a push notification delivered to Clara via SSE.
type Event struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// InteractiveDecision is delivered to a waiting interactive request.
type InteractiveDecision struct {
	Selection  string `json:"selection"`
	CustomText string `json:"custom_text,omitempty"`
	User       string `json:"user"`
}

// Router distributes Webex webhook events to SSE subscribers.
type Router struct {
	mu          sync.RWMutex
	subscribers map[string][]chan Event // keyed by machine name
	approvals   map[string]chan InteractiveDecision
}

// NewRouter creates an empty Router.
func NewRouter() *Router {
	return &Router{
		subscribers: make(map[string][]chan Event),
		approvals:   make(map[string]chan InteractiveDecision),
	}
}

// Subscribe registers a new SSE listener for the given machine.
// The returned cancel func must be called when the SSE connection closes.
func (r *Router) Subscribe(machine string) (<-chan Event, func()) {
	ch := make(chan Event, 32)
	r.mu.Lock()
	r.subscribers[machine] = append(r.subscribers[machine], ch)
	r.mu.Unlock()

	cancel := func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		subs := r.subscribers[machine]
		for i, s := range subs {
			if s == ch {
				r.subscribers[machine] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		close(ch)
	}
	return ch, cancel
}

// Publish sends an event to all subscribers across all machines.
// Webex webhooks are global (not machine-scoped) so we broadcast to every
// connected Clara instance and let the intent filter as needed.
func (r *Router) Publish(ev Event) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, subs := range r.subscribers {
		for _, ch := range subs {
			select {
			case ch <- ev:
			default: // slow subscriber — drop rather than block
			}
		}
	}
}

// RegisterInteractive creates a decision channel for the given request ID.
func (r *Router) RegisterInteractive(requestID string) <-chan InteractiveDecision {
	ch := make(chan InteractiveDecision, 1)
	r.mu.Lock()
	r.approvals[requestID] = ch
	r.mu.Unlock()
	return ch
}

// GetInteractiveChan returns the decision channel for a request ID, or nil if not found.
func (r *Router) GetInteractiveChan(requestID string) <-chan InteractiveDecision {
	r.mu.RLock()
	ch := r.approvals[requestID]
	r.mu.RUnlock()
	return ch
}

// ResolveInteractive delivers a decision for the given request ID.
// Returns true if a waiter was found.
func (r *Router) ResolveInteractive(requestID string, d InteractiveDecision) bool {
	r.mu.Lock()
	ch, ok := r.approvals[requestID]
	if ok {
		delete(r.approvals, requestID)
	}
	r.mu.Unlock()

	if ok {
		select {
		case ch <- d:
		default: 
		}
	}
	return ok
}

// WaitInteractive blocks until a decision arrives or the timeout elapses.
// Cleans up the stale entry on timeout.
func (r *Router) WaitInteractive(
	requestID string,
	ch <-chan InteractiveDecision,
	timeout time.Duration,
) (InteractiveDecision, bool) {
	select {
	case d := <-ch:
		return d, true
	case <-time.After(timeout):
		r.mu.Lock()
		delete(r.approvals, requestID)
		r.mu.Unlock()
		return InteractiveDecision{}, false
	}
}
