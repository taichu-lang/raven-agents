package event

import (
	"context"
	"sync"
)

type Subscription struct {
	ID      uintptr
	Channel chan *Event
}

type Bus interface {
	Subscribe() *Subscription
	Emit(ctx context.Context, event *Event) error
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

// Emit blocks while a subscriber is too slow to keep up, which pushes back on the producer instead
// of growing the buffer without bound. A channel send cannot be interrupted on its own, so the send
// also watches ctx: once the producer's context is done, the delivery is abandoned rather than
// parking the producer forever on a subscriber that has walked away, such as a disconnected HTTP
// stream.
func (b *MemoryBus) Emit(ctx context.Context, event *Event) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, s := range b.subscriptions {
		select {
		case s.Channel <- event:
		case <-ctx.Done():
			return ctx.Err()
		}
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
