package persistent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/internal/database"
	"github.com/uptrace/bun"
)

// ErrNilContext is returned when messages are stored without the conversation they belong to.
var ErrNilContext = errors.New("persistent: turn context is nil")

type StoreRef struct {
	db     *bun.DB
	logger *slog.Logger
}

func NewStoreRef(db *bun.DB, logger *slog.Logger) *StoreRef {
	return &StoreRef{
		db:     db,
		logger: logger,
	}
}

func (s *StoreRef) Close() error {
	if s.db != nil {
		return s.db.Close()
	}

	return nil
}

// StoreMessages persists the messages of a single conversation turn. Messages carrying a message id
// that is already stored are updated in place, which keeps the call idempotent when the agent loop
// replays the same assistant message across iterations.
func (s *StoreRef) StoreMessages(
	ctx context.Context,
	model string,
	messages []*underlying.Message,
) error {
	scope := TurnContextFromContext(ctx)
	if scope == nil {
		return errors.New("turn context is nil")
	}

	rows, err := buildRows(scope, model, messages)
	if err != nil {
		return err
	}

	if len(rows) == 0 {
		return nil
	}

	if _, err := s.db.NewInsert().
		Model(&rows).
		Exec(ctx); err != nil {
		return fmt.Errorf("store %d messages: %w", len(rows), err)
	}

	s.logger.DebugContext(ctx, "stored messages",
		"conversation_id", scope.ConversationID,
		"turn", scope.Turn,
		"count", len(rows),
	)

	return nil
}

func buildRows(scope *TurnContext, model string, messages []*underlying.Message) ([]MessageModel, error) {
	if scope == nil {
		return nil, ErrNilContext
	}

	createdAt := time.Now().Unix()
	rows := make([]MessageModel, 0, len(messages))
	for _, message := range messages {
		if message == nil {
			continue
		}

		content, err := encodeContents(message.Contents)
		if err != nil {
			return nil, err
		}

		// Tool result messages are built locally and carry no provider assigned id, generate one so
		// that they do not all collide on the unique message_id column.
		messageID := message.ID
		if messageID == "" {
			messageID = uuid.New().String()
		}

		rows = append(rows, MessageModel{
			MessageID:      messageID,
			ConversationID: scope.ConversationID,
			Turn:           scope.Turn,
			Role:           string(message.Role),
			Content:        content,
			Model:          model,
			Metadata:       database.EmptyJSONObject, // message state (interrupted, error), etc.
			CreatedAt:      createdAt,
		})
	}

	return rows, nil
}

func encodeContents(contents underlying.MessageContents) (json.RawMessage, error) {
	if len(contents) == 0 {
		return json.RawMessage(`[]`), nil
	}

	encoded, err := json.Marshal(contents)
	if err != nil {
		return nil, fmt.Errorf("encode message contents: %w", err)
	}

	return encoded, nil
}
