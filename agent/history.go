package agent

import (
	"context"

	"github.com/taichu-lang/raven-agents/agent/llm"
)

type HistoryProvider interface {
	Retrieve(ctx context.Context, user []*llm.Message) ([]*llm.Message, error)
	Store(ctx context.Context, response *llm.Message) error
}

func NewHistoryProvider() HistoryProvider {
	return NewMemoryHistoryProvider()
}

type InMemoryHistoryProvider struct {
	history []*llm.Message
	user    []*llm.Message
}

func NewMemoryHistoryProvider() HistoryProvider {
	return &InMemoryHistoryProvider{
		history: make([]*llm.Message, 0, 1),
		user:    nil,
	}
}

func (p *InMemoryHistoryProvider) Retrieve(ctx context.Context, user []*llm.Message) ([]*llm.Message, error) {
	p.user = user
	if len(p.history) == 0 {
		return user, nil
	}

	messages := append(p.history, user...)
	return messages, nil
}

func (p *InMemoryHistoryProvider) Store(ctx context.Context, response *llm.Message) error {
	p.history = append(p.history, p.user...)
	p.history = append(p.history, response)
	p.user = nil
	return nil
}
