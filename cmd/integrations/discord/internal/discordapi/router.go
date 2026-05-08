package discordapi

import (
	"encoding/json"
	"sync"
	"time"
)

// Event is a push notification sent to a Clara plugin over SSE.
type Event struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// ApprovalDecision is delivered to a waiting approval request.
type ApprovalDecision struct {
	Decision string `json:"decision"`
	User     string `json:"user"`
}

// Router distributes events to SSE subscribers and resolves pending approvals.
type Router struct {
	mu          sync.RWMutex
	subscribers map[string][]chan Event
	approvals   map[string]chan ApprovalDecision
}

func NewRouter() *Router {
	return &Router{
		subscribers: make(map[string][]chan Event),
		approvals:   make(map[string]chan ApprovalDecision),
	}
}

// Subscribe registers a new SSE subscriber for a machine.
// Returns the event channel and a cancel func to unsubscribe.
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

// Publish sends an event to all subscribers of the given machine.
// Empty machine broadcasts to all subscribers.
func (r *Router) Publish(machine string, ev Event) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var targets []chan Event
	if machine == "" {
		for _, subs := range r.subscribers {
			targets = append(targets, subs...)
		}
	} else {
		targets = r.subscribers[machine]
	}

	for _, ch := range targets {
		select {
		case ch <- ev:
		default: // drop if subscriber is slow
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
