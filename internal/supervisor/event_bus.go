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
	mu               sync.RWMutex
	nextID           uint64
	subscribers      map[uint64]chan Event
	cloudSubscribers map[uint64]chan CloudEvent
}

func NewEventBus() *EventBus {
	return &EventBus{
		subscribers:      make(map[uint64]chan Event),
		cloudSubscribers: make(map[uint64]chan CloudEvent),
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

func (b *EventBus) SubscribeCloud() (<-chan CloudEvent, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := b.nextID
	b.nextID++
	ch := make(chan CloudEvent, 100)
	b.cloudSubscribers[id] = ch

	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if existing, ok := b.cloudSubscribers[id]; ok {
			delete(b.cloudSubscribers, id)
			close(existing)
		}
	}
}

func (b *EventBus) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// Publish to legacy subscribers
	for _, ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			// Subscriber slow, skip
		}
	}

	// Wrap and publish to CloudEvent subscribers
	ce := ConvertToCloudEvent(event)
	for _, ch := range b.cloudSubscribers {
		select {
		case ch <- ce:
		default:
			// Subscriber slow, skip
		}
	}
}

func (b *EventBus) PublishCloud(ce CloudEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// Publish to CloudEvent subscribers
	for _, ch := range b.cloudSubscribers {
		select {
		case ch <- ce:
		default:
			// Subscriber slow, skip
		}
	}

	// Adapt and publish to legacy subscribers
	legacy := Event{
		Server: ce.Source,
		Method: ce.Type,
		Params: ce.Data,
	}
	for _, ch := range b.subscribers {
		select {
		case ch <- legacy:
		default:
			// Subscriber slow, skip
		}
	}
}
