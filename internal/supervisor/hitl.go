package supervisor

import (
	"context"
	"sync"

	"github.com/cockroachdb/errors"
)

// ResolutionOption represents a concrete choice the human can approve.
type ResolutionOption struct {
	ID          string `json:"id"`          // Option Identifier, e.g. "retry", "rollback", "patch"
	Description string `json:"description"` // Human-readable summary of what selecting this does
	ActionCode  string `json:"action_code"` // Executable script, code patch, or custom param override
}

// ApprovalRequest bundles problem context and discrete resolution options.
type ApprovalRequest struct {
	RequestID string             `json:"request_id"`
	Context   string             `json:"context"`
	Options   []ResolutionOption `json:"options"`
}

// SwappablePrompter routes approval prompts to the most active human interfaces.
type SwappablePrompter interface {
	Prompt(ctx context.Context, req ApprovalRequest) (ResolutionOption, error)
}

// ApprovalStore holds pending HITL approval requests in memory, indexed by
// RequestID. Decisions are delivered via a channel per request.
type ApprovalStore struct {
	mu      sync.RWMutex
	pending map[string]*pendingApproval
}

type pendingApproval struct {
	req    ApprovalRequest
	decide chan int // receives 1-based option index from CLI
}

// NewApprovalStore creates an empty ApprovalStore.
func NewApprovalStore() *ApprovalStore {
	return &ApprovalStore{pending: make(map[string]*pendingApproval)}
}

// Submit adds an approval request and blocks until a decision is received or
// ctx is cancelled. Returns the chosen ResolutionOption.
func (s *ApprovalStore) Submit(ctx context.Context, req ApprovalRequest) (ResolutionOption, error) {
	pa := &pendingApproval{req: req, decide: make(chan int, 1)}
	s.mu.Lock()
	s.pending[req.RequestID] = pa
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.pending, req.RequestID)
		s.mu.Unlock()
	}()

	select {
	case <-ctx.Done():
		return ResolutionOption{}, ctx.Err()
	case idx := <-pa.decide:
		if idx < 1 || idx > len(req.Options) {
			return ResolutionOption{}, errors.Newf("invalid option index %d", idx)
		}
		return req.Options[idx-1], nil
	}
}

// List returns all pending ApprovalRequests.
func (s *ApprovalStore) List() []ApprovalRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ApprovalRequest, 0, len(s.pending))
	for _, pa := range s.pending {
		out = append(out, pa.req)
	}
	return out
}

// Get returns the ApprovalRequest for id, or false if not found.
func (s *ApprovalStore) Get(id string) (ApprovalRequest, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	pa, ok := s.pending[id]
	if !ok {
		return ApprovalRequest{}, false
	}
	return pa.req, true
}

// Decide delivers option index idx (1-based) to the blocked Submit call.
// Returns an error if the request is not found or the index is out of range.
func (s *ApprovalStore) Decide(id string, idx int) error {
	s.mu.RLock()
	pa, ok := s.pending[id]
	s.mu.RUnlock()
	if !ok {
		return errors.Newf("approval request %q not found", id)
	}
	if idx < 1 || idx > len(pa.req.Options) {
		return errors.Newf("option %d out of range (1-%d)", idx, len(pa.req.Options))
	}
	select {
	case pa.decide <- idx:
		return nil
	default:
		return errors.New("approval already decided")
	}
}

// ActiveRouter acts as the central HITL gateway in Clara V2, routing requests to the best available prompters.
type ActiveRouter struct {
	mu        sync.RWMutex
	prompters map[string]SwappablePrompter
	activeID  string // ID of the currently preferred active prompter
}

// NewActiveRouter creates a new ActiveRouter.
func NewActiveRouter() *ActiveRouter {
	return &ActiveRouter{
		prompters: make(map[string]SwappablePrompter),
	}
}

// RegisterPrompter registers a new human-in-the-loop channel (e.g., "cli", "webex", "discord").
func (r *ActiveRouter) RegisterPrompter(id string, p SwappablePrompter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prompters[id] = p
	if r.activeID == "" {
		r.activeID = id // First registered becomes active by default
	}
}

// SetPreferredPrompter shifts routing preference manually.
func (r *ActiveRouter) SetPreferredPrompter(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.prompters[id]; !ok {
		return errors.Newf("prompter channel %q not registered", id)
	}
	r.activeID = id
	return nil
}

// Prompt satisfies SwappablePrompter by routing the interactive selection request.
func (r *ActiveRouter) Prompt(ctx context.Context, req ApprovalRequest) (ResolutionOption, error) {
	r.mu.RLock()
	activeID := r.activeID
	prompter, ok := r.prompters[activeID]
	r.mu.RUnlock()

	if !ok || prompter == nil {
		// Fallback check for any registered prompter
		r.mu.RLock()
		defer r.mu.RUnlock()
		for _, p := range r.prompters {
			if p != nil {
				return p.Prompt(ctx, req)
			}
		}
		return ResolutionOption{}, errors.New("no active Human-In-The-Loop prompter channels registered")
	}

	return prompter.Prompt(ctx, req)
}

