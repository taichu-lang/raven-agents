package event

import "sync"

type EventHandler func(event *Event)

type Bus interface {
	Subscribe(handler EventHandler) (uintptr, error)
	Emit(event *Event) error
	Unsubscribe(handle uintptr) error
}

type MemoryBus struct {
	mu       sync.RWMutex
	handlers map[uintptr]EventHandler
	id       uintptr
}

func NewMemoryBus() *MemoryBus {
	return &MemoryBus{
		handlers: make(map[uintptr]EventHandler),
		id:       0,
	}
}

func (b *MemoryBus) Subscribe(handler EventHandler) (uintptr, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.id++
	b.handlers[b.id] = handler
	return b.id, nil
}

func (b *MemoryBus) Emit(event *Event) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, handler := range b.handlers {
		handler(event)
	}
	return nil
}

func (b *MemoryBus) Unsubscribe(handle uintptr) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.handlers, handle)
	return nil
}
