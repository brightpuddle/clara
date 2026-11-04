package agents

import (
	"encoding/json"
	"time"

	"github.com/cockroachdb/errors"
)

// EventType names a kind of logged (or live-only) event.
type EventType string

const (
	TypeHostStatus         EventType = "host.status"
	TypeSessionCreated     EventType = "session.created"
	TypeSessionUpdated     EventType = "session.updated"
	TypeSessionStatus      EventType = "session.status"
	TypeMessageUser        EventType = "message.user"
	TypeMessageAssistant   EventType = "message.assistant"
	TypeMessageDelta       EventType = "message.delta"
	TypeTurnStarted        EventType = "turn.started"
	TypeTurnEnded          EventType = "turn.ended"
	TypeToolCall           EventType = "tool.call"
	TypeToolUpdate         EventType = "tool.update"
	TypePromptQueued       EventType = "prompt.queued"
	TypePromptSent         EventType = "prompt.sent"
	TypePromptCancelled    EventType = "prompt.cancelled"
	TypeAttentionRequested EventType = "attention.requested"
	TypeAttentionResolved  EventType = "attention.resolved"
	TypeUsageUpdated       EventType = "usage.updated"
)

// AllEventTypes lists every known event type.
func AllEventTypes() []EventType {
	return []EventType{
		TypeHostStatus, TypeSessionCreated, TypeSessionUpdated, TypeSessionStatus,
		TypeMessageUser, TypeMessageAssistant, TypeMessageDelta,
		TypeTurnStarted, TypeTurnEnded, TypeToolCall, TypeToolUpdate,
		TypePromptQueued, TypePromptSent, TypePromptCancelled,
		TypeAttentionRequested, TypeAttentionResolved, TypeUsageUpdated,
	}
}

// Logged reports whether events of this type are persisted in the host log.
// Streaming deltas are live-only.
func (t EventType) Logged() bool { return t != TypeMessageDelta }

type SessionState string

const (
	StateStarting SessionState = "starting"
	StateIdle     SessionState = "idle"
	StateRunning  SessionState = "running"
	StateBlocked  SessionState = "blocked"
	StateFailed   SessionState = "failed"
	StateClosed   SessionState = "closed"
)

func (s SessionState) Valid() bool {
	switch s {
	case StateStarting, StateIdle, StateRunning, StateBlocked, StateFailed, StateClosed:
		return true
	}
	return false
}

type StopReason string

const (
	StopEndTurn     StopReason = "end_turn"
	StopCancelled   StopReason = "cancelled"
	StopInterrupted StopReason = "interrupted"
	StopError       StopReason = "error"
)

type SessionKind string

const (
	KindLongLived SessionKind = "longlived"
	KindTask      SessionKind = "task"
)

type Origin string

const (
	OriginManaged  Origin = "managed"
	OriginObserved Origin = "observed"
)

type AttentionKind string

const (
	AttentionQuestion  AttentionKind = "question"
	AttentionTurnEnded AttentionKind = "turn_ended"
)

// Event is the envelope for everything in the log. Seq is assigned by the hub,
// HostSeq by the host log.
type Event struct {
	Seq     int64           `json:"seq,omitempty"`
	HostSeq int64           `json:"host_seq,omitempty"`
	ID      string          `json:"id"`
	Host    string          `json:"host"`
	Session string          `json:"session,omitempty"`
	TS      time.Time       `json:"ts"`
	Type    EventType       `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// NewEvent builds an envelope with a fresh ID and UTC timestamp. The payload
// must be the struct that matches typ.
func NewEvent(host, session string, typ EventType, payload any) (Event, error) {
	want := newPayload(typ)
	if want == nil {
		return Event{}, errors.Newf("unknown event type %q", typ)
	}
	if !sameType(want, payload) {
		return Event{}, errors.Newf("payload %T does not match event type %q", payload, typ)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return Event{}, errors.Wrap(err, "marshal event payload")
	}
	return Event{
		ID:      NewID(),
		Host:    host,
		Session: session,
		TS:      time.Now().UTC(),
		Type:    typ,
		Payload: raw,
	}, nil
}

// Decode returns the typed payload (a value, not a pointer).
func (e Event) Decode() (any, error) {
	p := newPayload(e.Type)
	if p == nil {
		return nil, errors.Newf("unknown event type %q", e.Type)
	}
	return decodeInto(p, e.Payload)
}

type Attachment struct {
	Name string `json:"name,omitempty"`
	Mime string `json:"mime,omitempty"`
	Ref  string `json:"ref,omitempty"`
}

type Usage struct {
	InputTokens  int64    `json:"input_tokens,omitempty"`
	OutputTokens int64    `json:"output_tokens,omitempty"`
	TotalTokens  int64    `json:"total_tokens,omitempty"`
	Cost         *float64 `json:"cost,omitempty"`
}

type ToolsSummary struct {
	Count int      `json:"count"`
	Last  []string `json:"last,omitempty"`
}

type Blocked struct {
	Kind     string     `json:"kind"`
	ResetsAt *time.Time `json:"resets_at,omitempty"`
}

type Option struct {
	ID    string `json:"id,omitempty"`
	Label string `json:"label"`
}

type NativeRef struct {
	FormID string `json:"form_id,omitempty"`
}

type HostStatus struct {
	Online   bool      `json:"online"`
	LastSeen time.Time `json:"last_seen"`
}

type SessionCreated struct {
	Agent  string      `json:"agent"`
	Dir    string      `json:"dir"`
	Title  string      `json:"title,omitempty"`
	Kind   SessionKind `json:"kind"`
	Model  string      `json:"model,omitempty"`
	Origin Origin      `json:"origin"`
	// NativeID is the agent's own session id, needed to resume after a restart.
	NativeID string `json:"native_id,omitempty"`
}

type SessionUpdated struct {
	Title    string      `json:"title,omitempty"`
	Model    string      `json:"model,omitempty"`
	Kind     SessionKind `json:"kind,omitempty"`
	NativeID string      `json:"native_id,omitempty"`
}

type SessionStatus struct {
	State   SessionState `json:"state"`
	Reason  string       `json:"reason,omitempty"`
	Blocked *Blocked     `json:"blocked,omitempty"`
}

type MessageUser struct {
	MessageID   string       `json:"message_id"`
	Text        string       `json:"text"`
	Attachments []Attachment `json:"attachments,omitempty"`
	Origin      string       `json:"origin,omitempty"`
	PromptID    string       `json:"prompt_id,omitempty"`
}

type MessageAssistant struct {
	MessageID string `json:"message_id"`
	TurnID    string `json:"turn_id"`
	Text      string `json:"text"`
}

// MessageDelta is live-only and never written to the log.
type MessageDelta struct {
	MessageID string `json:"message_id"`
	Text      string `json:"text"`
}

type TurnStarted struct {
	TurnID   string `json:"turn_id"`
	PromptID string `json:"prompt_id,omitempty"`
}

type TurnEnded struct {
	TurnID       string       `json:"turn_id"`
	StopReason   StopReason   `json:"stop_reason"`
	Usage        *Usage       `json:"usage,omitempty"`
	ToolsSummary ToolsSummary `json:"tools_summary"`
}

type ToolCall struct {
	CallID    string   `json:"call_id"`
	TurnID    string   `json:"turn_id"`
	Name      string   `json:"name,omitempty"`
	Kind      string   `json:"kind,omitempty"`
	Title     string   `json:"title,omitempty"`
	Locations []string `json:"locations,omitempty"`
	Status    string   `json:"status,omitempty"`
}

type ToolUpdate struct {
	CallID        string `json:"call_id"`
	Status        string `json:"status"`
	OutputSummary string `json:"output_summary,omitempty"`
	BlobID        string `json:"blob_id,omitempty"`
}

type PromptQueued struct {
	PromptID string `json:"prompt_id"`
	Text     string `json:"text"`
	Position int    `json:"position"`
	Origin   string `json:"origin,omitempty"`
}

type PromptSent struct {
	PromptID string `json:"prompt_id"`
}

type PromptCancelled struct {
	PromptID string `json:"prompt_id"`
}

type AttentionRequested struct {
	AttentionID string        `json:"attention_id"`
	Kind        AttentionKind `json:"kind"`
	Title       string        `json:"title,omitempty"`
	Options     []Option      `json:"options,omitempty"`
	AllowText   bool          `json:"allow_text,omitempty"`
	Native      *NativeRef    `json:"native,omitempty"`
}

type AttentionResolved struct {
	AttentionID string `json:"attention_id"`
	By          string `json:"by"`
	Answer      string `json:"answer,omitempty"`
}

type UsageUpdated struct {
	Cost   *float64 `json:"cost,omitempty"`
	Tokens Usage    `json:"tokens"`
}

// newPayload returns a zero value of the payload struct for typ, or nil.
func newPayload(typ EventType) any {
	switch typ {
	case TypeHostStatus:
		return HostStatus{}
	case TypeSessionCreated:
		return SessionCreated{}
	case TypeSessionUpdated:
		return SessionUpdated{}
	case TypeSessionStatus:
		return SessionStatus{}
	case TypeMessageUser:
		return MessageUser{}
	case TypeMessageAssistant:
		return MessageAssistant{}
	case TypeMessageDelta:
		return MessageDelta{}
	case TypeTurnStarted:
		return TurnStarted{}
	case TypeTurnEnded:
		return TurnEnded{}
	case TypeToolCall:
		return ToolCall{}
	case TypeToolUpdate:
		return ToolUpdate{}
	case TypePromptQueued:
		return PromptQueued{}
	case TypePromptSent:
		return PromptSent{}
	case TypePromptCancelled:
		return PromptCancelled{}
	case TypeAttentionRequested:
		return AttentionRequested{}
	case TypeAttentionResolved:
		return AttentionResolved{}
	case TypeUsageUpdated:
		return UsageUpdated{}
	}
	return nil
}
