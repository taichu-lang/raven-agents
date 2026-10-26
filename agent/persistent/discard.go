package persistent

import (
	"context"
	"errors"
	"time"

	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
)

type DiscardStore struct{}

func (d *DiscardStore) AddTurn(
	ctx context.Context,
	turnContext *TurnContext,
	userMessageID string,
) (*TurnModel, error) {
	if turnContext == nil {
		return nil, errors.New("turn context is nil")
	}

	now := time.Now().Unix()
	return &TurnModel{
		ConversationID: turnContext.ConversationID,
		TurnSeq:        1,
		UserMessageID:  userMessageID,
		Status:         TurnStatusRunning,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

func (d *DiscardStore) UpdateTurn(
	ctx context.Context,
	turnContext *TurnContext,
) error {
	return nil
}

func (d *DiscardStore) LoadLatestTurn(ctx context.Context, conversationID string) (*TurnModel, error) {
	return nil, nil
}

func (d *DiscardStore) StoreMessages(
	ctx context.Context,
	turnContext *TurnContext,
	messages []*underlying.Message,
) error {
	return nil
}

func (d *DiscardStore) LoadMessages(ctx context.Context, conversationID string) ([]*underlying.Message, error) {
	return []*underlying.Message{}, nil
}

func (d *DiscardStore) Close() error {
	return nil
}
