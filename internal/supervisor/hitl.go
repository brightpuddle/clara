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
