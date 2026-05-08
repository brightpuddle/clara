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

// ApprovalDecision is delivered to a waiting approval request.
type ApprovalDecision struct {
	Decision string `json:"decision"`
	User     string `json:"user"`
}

// Router distributes Webex webhook events to SSE subscribers.
type Router struct {
	mu          sync.RWMutex
	subscribers map[string][]chan Event // keyed by machine name
	approvals   map[string]chan ApprovalDecision
}

// NewRouter creates an empty Router.
func NewRouter() *Router {
	return &Router{
		subscribers: make(map[string][]chan Event),
		approvals:   make(map[string]chan ApprovalDecision),
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

// RegisterApproval creates a decision channel for the given request ID.
func (r *Router) RegisterApproval(requestID string) <-chan ApprovalDecision {
	ch := make(chan ApprovalDecision, 1)
	r.mu.Lock()
	r.approvals[requestID] = ch
	r.mu.Unlock()
	return ch
}

// GetApprovalChan returns the decision channel for a request ID, or nil if not found.
func (r *Router) GetApprovalChan(requestID string) <-chan ApprovalDecision {
	r.mu.RLock()
	ch := r.approvals[requestID]
	r.mu.RUnlock()
	return ch
}

// ResolveApproval delivers a decision for the given request ID.
// Returns true if a waiter was found.
func (r *Router) ResolveApproval(requestID string, d ApprovalDecision) bool {
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

// WaitApproval blocks until a decision arrives or the timeout elapses.
// Cleans up the stale entry on timeout.
func (r *Router) WaitApproval(
	requestID string,
	ch <-chan ApprovalDecision,
	timeout time.Duration,
) (ApprovalDecision, bool) {
	select {
	case d := <-ch:
		return d, true
	case <-time.After(timeout):
		r.mu.Lock()
		delete(r.approvals, requestID)
		r.mu.Unlock()
		return ApprovalDecision{}, false
	}
}