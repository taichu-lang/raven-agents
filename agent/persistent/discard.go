package persistent

import (
	"context"
	"errors"
	"time"

	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/pkg/database"
)

type DiscardStore struct{}

func (d *DiscardStore) AddTurn(
	ctx context.Context,
	turnContext *TurnContext,
	userMessageID string,
) (*database.TurnModel, error) {
	if turnContext == nil {
		return nil, errors.New("turn context is nil")
	}

	now := time.Now().Unix()
	return &database.TurnModel{
		ConversationID: turnContext.ConversationID,
		TurnSeq:        1,
		UserMessageID:  userMessageID,
		Status:         database.TurnStatusRunning,
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

func (d *DiscardStore) LoadLatestTurn(ctx context.Context, conversationID string) (*database.TurnModel, error) {
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

func (d *DiscardStore) LoadMessagesAfter(
	ctx context.Context,
	conversationID string,
	afterMessageID string,
) ([]*underlying.Message, error) {
	return []*underlying.Message{}, nil
}

func (d *DiscardStore) StoreSummary(
	ctx context.Context,
	turnContext *TurnContext,
	summary *SummaryModel,
) error {
	return nil
}

func (d *DiscardStore) LoadLatestSummary(ctx context.Context, conversationID string) (*SummaryModel, error) {
	return nil, nil
}

func (d *DiscardStore) Close() error {
	return nil
}
