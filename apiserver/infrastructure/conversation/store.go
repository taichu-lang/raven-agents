package conversation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/taichu-lang/raven-agents/internal/database"
	"github.com/uptrace/bun"
)

const defaultLimit = 20

// ErrConversationNotFound is returned when the targeted conversation does not exist.
var ErrConversationNotFound = errors.New("conversation: not found")

type ConversationStore struct {
	db     *bun.DB
	logger *slog.Logger
}

func NewConversationStore(db *bun.DB, logger *slog.Logger) *ConversationStore {
	return &ConversationStore{
		db:     db,
		logger: logger,
	}
}

// Create persists a new conversation. The caller owns the conversation id, the creation timestamp
// is assigned here when it is left empty.
func (s *ConversationStore) Create(ctx context.Context, conversation *ConversationModel) error {
	if conversation == nil {
		return errors.New("conversation: model is nil")
	}

	if conversation.ConversationID == "" {
		return errors.New("conversation: conversation id is required")
	}

	if len(conversation.Metadata) == 0 {
		conversation.Metadata = database.EmptyJSONObject
	}

	if conversation.CreatedAt == 0 {
		conversation.CreatedAt = time.Now().Unix()
	}

	if _, err := s.db.NewInsert().
		Model(conversation).
		Exec(ctx); err != nil {
		return fmt.Errorf("create conversation %q: %w", conversation.ConversationID, err)
	}

	s.logger.DebugContext(ctx, "created conversation",
		"conversation_id", conversation.ConversationID,
		"user_id", conversation.UserID,
	)

	return nil
}

// UpdateTitle renames the conversation.
func (s *ConversationStore) UpdateTitle(ctx context.Context, conversationID, title string) error {
	result, err := s.db.NewUpdate().
		Model((*ConversationModel)(nil)).
		Set("title = ?", title).
		Where("conversation_id = ?", conversationID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("update title of conversation %q: %w", conversationID, err)
	}

	return affected(result, conversationID)
}

// UpdateMetadata replaces the metadata of the conversation.
func (s *ConversationStore) UpdateMetadata(
	ctx context.Context,
	conversationID string,
	metadata json.RawMessage,
) error {
	if len(metadata) == 0 {
		metadata = database.EmptyJSONObject
	}

	result, err := s.db.NewUpdate().
		Model((*ConversationModel)(nil)).
		Set("metadata = ?", metadata).
		Where("conversation_id = ?", conversationID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("update metadata of conversation %q: %w", conversationID, err)
	}

	return affected(result, conversationID)
}

// Delete removes the conversation. The messages of the conversation are not touched, they are owned
// by the message store.
func (s *ConversationStore) Delete(ctx context.Context, conversationID string) error {
	result, err := s.db.NewDelete().
		Model((*ConversationModel)(nil)).
		Where("conversation_id = ?", conversationID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete conversation %q: %w", conversationID, err)
	}

	return affected(result, conversationID)
}

func (s *ConversationStore) Get(ctx context.Context, conversationID string) (*ConversationModel, error) {
	conversation := &ConversationModel{}
	if err := s.db.NewSelect().Model(conversation).Where("conversation_id = ?", conversationID).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}

		return nil, err
	}

	return conversation, nil
}

// List returns one page of the conversations of a user, the most recently created first. A limit
// below one falls back to defaultListLimit.
func (s *ConversationStore) List(
	ctx context.Context,
	userID int64,
	limit, page int64,
) ([]*ConversationModel, error) {
	if limit <= 0 {
		limit = defaultLimit
	}

	if page < 0 {
		page = 0
	}

	offset := (page - 1) * limit
	conversations := make([]*ConversationModel, 0, limit)
	if err := s.db.NewSelect().
		Model(&conversations).
		Where("user_id = ?", userID).
		Order("id DESC").
		Limit(limit).
		Offset(offset).
		Scan(ctx); err != nil {
		return nil, fmt.Errorf("list conversations of user %d: %w", userID, err)
	}

	return conversations, nil
}

// affected turns an update or delete that matched no row into ErrConversationNotFound.
func affected(result sql.Result, conversationID string) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected of conversation %q: %w", conversationID, err)
	}

	if rows == 0 {
		return fmt.Errorf("%w: %s", ErrConversationNotFound, conversationID)
	}

	return nil
}
