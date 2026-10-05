package event

import "sync"

type Subscription struct {
	ID      uintptr
	Channel chan *Event
}

type Bus interface {
	Subscribe() *Subscription
	Emit(event *Event) error
	Unsubscribe(id uintptr) error
}

type MemoryBus struct {
	mu            sync.RWMutex
	subscriptions map[uintptr]*Subscription
	id            uintptr
}

func NewMemoryBus() *MemoryBus {
	return &MemoryBus{
		subscriptions: make(map[uintptr]*Subscription),
		id:            0,
	}
}

func (b *MemoryBus) Subscribe() *Subscription {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.id++
	s := &Subscription{
		ID:      b.id,
		Channel: make(chan *Event, 64),
	}
	b.subscriptions[b.id] = s
	return s
}

func (b *MemoryBus) Emit(event *Event) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, s := range b.subscriptions {
		s.Channel <- event
	}

	return nil
}

// Unsubscribe removes the subscription and closes its channel, so that the consumer can drain the
// events still buffered in the channel and then exit its receive loop. Emit holds the read lock
// while sending, so removing the subscription under the write lock guarantees no send races with
// the close.
func (b *MemoryBus) Unsubscribe(id uintptr) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	s, ok := b.subscriptions[id]
	if !ok {
		// Already unsubscribed; closing the channel twice would panic.
		return nil
	}

	delete(b.subscriptions, id)
	close(s.Channel)
	return nil
}
