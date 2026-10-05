package persistent

import (
	"context"

	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
)

type DiscardStore struct{}

func (d *DiscardStore) StoreMessages(ctx context.Context, model string, messages []*underlying.Message) error {
	return nil
}

func (d *DiscardStore) Close() error {
	return nil
}
