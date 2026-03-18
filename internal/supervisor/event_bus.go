package supervisor

import (
	"sync"
)

type Event struct {
	Server string
	Method string
	Params any
}

type EventBus struct {
	mu          sync.RWMutex
	nextID      uint64
	subscribers map[uint64]chan Event
}

func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[uint64]chan Event),
	}
}

func (b *EventBus) Subscribe() (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := b.nextID
	b.nextID++
	ch := make(chan Event, 100)
	b.subscribers[id] = ch

	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if existing, ok := b.subscribers[id]; ok {
			delete(b.subscribers, id)
			close(existing)
		}
	}
}

func (b *EventBus) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			// Subscriber slow, skip
		}
	}
}
