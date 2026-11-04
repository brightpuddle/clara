// Package adapter drives coding agents behind a normalised session interface.
// The ACP implementation covers every agent that speaks the Agent Client
// Protocol over stdio (ADR 0005); per-agent differences live in Quirks.
package adapter

import (
	"context"
	"time"

	"github.com/cockroachdb/errors"

	"github.com/brightpuddle/clara/internal/agents"
)

// ErrBusy is returned by Prompt while an earlier prompt is unanswered. Queueing
// belongs to the session runner above the adapter.
var ErrBusy = errors.New("session already has a prompt in flight")

// Adapter creates and discovers agent sessions for one agent type on one host.
type Adapter interface {
	Name() string
	// Open starts a new session, or resumes one when spec.ResumeID is set. The
	// ctx bounds the session's lifetime: cancelling it closes the session.
	Open(ctx context.Context, spec OpenSpec) (AgentSession, error)
	// List returns resumable native sessions.
	List(ctx context.Context) ([]NativeSession, error)
}

type OpenSpec struct {
	Dir      string
	Model    string
	ResumeID string // native session id; empty for a new session
}

type NativeSession struct {
	ID        string    `json:"id"`
	Title     string    `json:"title,omitempty"`
	Dir       string    `json:"dir,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitzero"`
}

type Prompt struct {
	ID   string // caller's prompt id, echoed on TurnEnded
	Text string
}

// AgentSession is one live conversation. It handles one prompt at a time.
type AgentSession interface {
	NativeID() string
	// Prompt writes the prompt to the agent and returns; the outcome arrives as
	// TurnEnded on Events. It returns ErrBusy if a prompt is still in flight.
	Prompt(ctx context.Context, p Prompt) error
	// Cancel asks the agent to stop the running turn. The turn still ends with a
	// TurnEnded event (normally StopCancelled).
	Cancel(ctx context.Context) error
	// Events carries normalised events and is closed after Exited. The consumer
	// must drain it continuously; a stalled consumer stalls the session.
	Events() <-chan AgentEvent
	Close(ctx context.Context) error
}

// AgentEvent is a normalised event from an agent session.
type AgentEvent interface{ agentEvent() }

// MessageDelta is a streamed fragment of assistant text (live only).
type MessageDelta struct{ MessageID, Text string }

// MessageDone is a finished assistant message.
type MessageDone struct{ MessageID, Text string }

type ToolCall struct {
	CallID    string
	Name      string
	Kind      string
	Title     string
	Locations []string
	Status    string
}

type ToolUpdate struct {
	CallID        string
	Status        string
	Title         string
	OutputSummary string
}

// TurnEnded reports the answer to a prompt. Err is set (with StopError) when the
// agent rejected the prompt or failed it.
type TurnEnded struct {
	PromptID   string
	StopReason agents.StopReason
	Usage      *agents.Usage
	Err        string
}

// Blocked means the agent cannot proceed for an external reason (for example a
// quota). It precedes the TurnEnded of the same turn.
type Blocked struct{ agents.Blocked }

type UsageUpdate struct {
	Used int64
	Size int64
	Cost *float64
}

type TitleUpdate struct{ Title string }

// Exited is always the last event. Err is nil for a requested close.
type Exited struct{ Err error }

func (MessageDelta) agentEvent() {}
func (MessageDone) agentEvent()  {}
func (ToolCall) agentEvent()     {}
func (ToolUpdate) agentEvent()   {}
func (TurnEnded) agentEvent()    {}
func (Blocked) agentEvent()      {}
func (UsageUpdate) agentEvent()  {}
func (TitleUpdate) agentEvent()  {}
func (Exited) agentEvent()       {}
