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
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
)

// ErrNilScope is returned when messages are stored without the conversation they belong to.
var ErrNilScope = errors.New("persistent: nil scope")

// emptyJSONObject is the fallback for the NOT NULL json columns.
var emptyJSONObject = json.RawMessage(`{}`)

// store holds the dialect independent behavior shared by PostgresStore and SqliteStore.
type store struct {
	db         *bun.DB
	migrations *migrate.Migrations
	logger     *slog.Logger
}

// DB exposes the underlying bun database, so that callers can run custom queries.
func (s *store) DB() *bun.DB {
	return s.db
}

func (s *store) Close() error {
	return s.db.Close()
}

// Migrate applies the pending schema migrations of the store dialect.
func (s *store) Migrate(ctx context.Context) error {
	migrator := migrate.NewMigrator(s.db, s.migrations)
	if err := migrator.Init(ctx); err != nil {
		return fmt.Errorf("init migrator: %w", err)
	}

	group, err := migrator.Migrate(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	if group.IsZero() {
		s.logger.DebugContext(ctx, "schema already up to date")
		return nil
	}

	s.logger.InfoContext(ctx, "applied migrations", "group", group.String())
	return nil
}

// StoreMessages persists the messages of a single conversation turn. Messages carrying a message id
// that is already stored are updated in place, which keeps the call idempotent when the agent loop
// replays the same assistant message across iterations.
func (s *store) StoreMessages(
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
		return nil, ErrNilScope
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
			Metadata:       emptyJSONObject, // message state (interrupted, error), etc.
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
