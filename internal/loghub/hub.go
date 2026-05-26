// Package loghub provides the central ring-buffer hub used by the daemon to
// publish structured log entries for all three observability streams:
// events, evaluator decisions, and actuator subprocess logs.
package loghub

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/brightpuddle/clara/internal/ringbuf"
)

const defaultCap = 1000

// Hub holds three named ring buffers: one per observability stream.
type Hub struct {
	Event     *ringbuf.RingBuffer
	Evaluator *ringbuf.RingBuffer
	Actuator  *ringbuf.RingBuffer

	// Per-actuator sub-buffers for actuator.logs <id>.
	mu       sync.RWMutex
	actuators map[string]*ringbuf.RingBuffer
}

// New creates a Hub with default ring buffer capacity.
func New() *Hub {
	return &Hub{
		Event:     ringbuf.New(defaultCap),
		Evaluator: ringbuf.New(defaultCap),
		Actuator:  ringbuf.New(defaultCap),
		actuators: make(map[string]*ringbuf.RingBuffer),
	}
}

// entry is the wire format for a single stream log line.
type entry struct {
	Stream     string `json:"stream"`
	Time       string `json:"time"`
	Type       string `json:"type,omitempty"`
	Source     string `json:"source,omitempty"`
	ActuatorID string `json:"id,omitempty"`
	Level      string `json:"level,omitempty"`
	Msg        string `json:"msg"`
	Data       any    `json:"data,omitempty"`
}

func marshal(e entry) json.RawMessage {
	b, _ := json.Marshal(e)
	return b
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

// PushEvent publishes a CloudEvent to the event ring buffer.
func (h *Hub) PushEvent(eventType, source string, data any) {
	h.Event.Push(marshal(entry{
		Stream: "event",
		Time:   now(),
		Type:   eventType,
		Source: source,
		Data:   data,
	}))
}

// PushEvaluator publishes an evaluator decision log entry.
func (h *Hub) PushEvaluator(level, msg string, fields map[string]any) {
	h.Evaluator.Push(marshal(entry{
		Stream: "evaluator",
		Time:   now(),
		Level:  level,
		Msg:    msg,
		Data:   fields,
	}))
}

// PushActuator publishes to the global actuator log and to the per-actuator buffer.
func (h *Hub) PushActuator(actuatorID, level, msg string, data any) {
	e := marshal(entry{
		Stream:     "actuator",
		Time:       now(),
		ActuatorID: actuatorID,
		Level:      level,
		Msg:        msg,
		Data:       data,
	})
	h.Actuator.Push(e)
	h.bufFor(actuatorID).Push(e)
}

// BufFor returns (creating if needed) the per-actuator ring buffer.
func (h *Hub) BufFor(id string) *ringbuf.RingBuffer {
	return h.bufFor(id)
}

func (h *Hub) bufFor(id string) *ringbuf.RingBuffer {
	h.mu.RLock()
	buf, ok := h.actuators[id]
	h.mu.RUnlock()
	if ok {
		return buf
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if buf, ok = h.actuators[id]; ok {
		return buf
	}
	buf = ringbuf.New(defaultCap)
	h.actuators[id] = buf
	return buf
}
