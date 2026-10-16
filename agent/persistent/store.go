package persistent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/internal/database"
	"github.com/uptrace/bun"
)

// ErrNilContext is returned when messages are stored without the conversation they belong to.
var ErrNilContext = errors.New("persistent: turn context is nil")

// ErrTurnNotFound is returned when an update matches no turn row.
var ErrTurnNotFound = errors.New("persistent: turn not found")

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

// addTurnMaxAttempts bounds the retries when concurrent AddTurn calls on the same conversation race
// for the next turn_seq and all but one hit the unique index on (conversation_id, turn_seq).
const addTurnMaxAttempts = 3

// AddTurn appends a new turn to the conversation, assigning it the next contiguous turn_seq
// (1, 2, 3, ...), and returns the stored turn. Turn rows are never deleted, so MAX + 1 stays
// contiguous.
func (s *StoreRef) AddTurn(
	ctx context.Context,
	turnContext *TurnContext,
	userMessageID string,
) (*TurnModel, error) {
	if turnContext == nil {
		return nil, errors.New("turn context is nil")
	}

	var err error
	for attempt := 0; attempt < addTurnMaxAttempts; attempt++ {
		var turn *TurnModel
		turn, err = s.insertNextTurn(ctx, turnContext.ConversationID, userMessageID)
		if err == nil {
			turnContext.TurnSeq = turn.TurnSeq
			return turn, nil
		}

		if !database.IsUniqueViolation(err) {
			break
		}
	}

	return nil, fmt.Errorf("add turn of conversation %s: %w", turnContext.ConversationID, err)
}

// insertNextTurn performs a single AddTurn attempt.
func (s *StoreRef) insertNextTurn(
	ctx context.Context,
	conversationID string,
	userMessageID string,
) (*TurnModel, error) {
	now := time.Now().Unix()
	turn := &TurnModel{
		ConversationID: conversationID,
		UserMessageID:  userMessageID,
		Status:         TurnStatusRunning,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	// The next turn_seq is computed inside the INSERT itself: a single statement is atomic, so
	// concurrent inserts on the same conversation cannot both observe the same MAX without one of
	// them losing the unique index race, which AddTurn then retries.
	if _, err := s.db.NewInsert().
		Model(turn).
		Value(
			"turn_seq",
			`COALESCE((SELECT MAX(turn_seq) FROM turns WHERE conversation_id = ?), 0) + 1`,
			conversationID,
		).
		Exec(ctx); err != nil {
		return nil, err
	}

	// turn_seq was computed by the database, read the stored row back to pick it up.
	if err := s.db.NewSelect().Model(turn).WherePK().Scan(ctx); err != nil {
		return nil, err
	}

	s.logger.DebugContext(ctx, "added turn",
		"conversation_id", conversationID,
		"turn", turn.TurnSeq,
		"id", turn.ID,
	)

	return turn, nil
}

// UpdateTurn syncs the mutable state the agent loop accumulated on the TurnContext (status,
// iterations, tokens) back to the stored turn, located by the (conversation_id, turn_seq) pair
// that AddTurn assigned. Columns the TurnContext does not own (lease, epoch, cancel flag, message
// ids) are left untouched.
func (s *StoreRef) UpdateTurn(ctx context.Context, turnContext *TurnContext) error {
	if turnContext == nil {
		return errors.New("turn context is nil")
	}

	turn := &TurnModel{
		Status:     turnContext.Status,
		Iterations: turnContext.Iterations,
		TokensUsed: turnContext.TokenUsage,
		UpdatedAt:  time.Now().Unix(),
	}

	result, err := s.db.NewUpdate().
		Model(turn).
		Column("status", "iterations", "tokens_used", "updated_at").
		Where("conversation_id = ?", turnContext.ConversationID).
		Where("turn_seq = ?", turnContext.TurnSeq).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("update turn %d of conversation %s: %w",
			turnContext.TurnSeq, turnContext.ConversationID, err)
	}

	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return fmt.Errorf("update turn %d of conversation %s: %w",
			turnContext.TurnSeq, turnContext.ConversationID, ErrTurnNotFound)
	}

	s.logger.DebugContext(ctx, "updated turn",
		"conversation_id", turnContext.ConversationID,
		"turn", turnContext.TurnSeq,
		"status", turnContext.Status,
	)

	return nil
}

// StoreMessages persists the messages of a single conversation turn. Messages carrying a message id
// that is already stored are updated in place, which keeps the call idempotent when the agent loop
// replays the same assistant message across iterations.
func (s *StoreRef) StoreMessages(
	ctx context.Context,
	turnContext *TurnContext,
	messages []*underlying.Message,
) error {
	if turnContext == nil {
		return errors.New("turn context is nil")
	}

	rows, err := buildMessageRows(turnContext, messages)
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
		"conversation_id", turnContext.ConversationID,
		"turn", turnContext.TurnSeq,
		"count", len(rows),
	)

	return nil
}

func buildMessageRows(
	ctx *TurnContext,
	messages []*underlying.Message,
) ([]MessageModel, error) {
	if ctx == nil {
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

		rows = append(rows, MessageModel{
			MessageID:      message.ID,
			ConversationID: ctx.ConversationID,
			TurnSeq:        ctx.TurnSeq,
			Role:           string(message.Role),
			Content:        content,
			Model:          ctx.Model,
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
