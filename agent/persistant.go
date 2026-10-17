package agent

import (
	"context"
	"log/slog"

	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/agent/persistent"
	"github.com/taichu-lang/raven-agents/internal/database"
	"github.com/uptrace/bun"
)

type Repository interface {
	// AddTurn appends a new turn to the conversation, assigning it the next contiguous turn_seq
	// (1, 2, 3, ...), and returns the stored turn.
	AddTurn(
		ctx context.Context,
		turnContext *persistent.TurnContext,
		userMessageID string,
	) (*persistent.TurnModel, error)

	UpdateTurn(ctx context.Context, turnContext *persistent.TurnContext) error

	// StoreMessages persists the messages produced by a single conversation turn. Scope carries the
	// conversation attributes that an underlying.Message does not know about.
	StoreMessages(
		ctx context.Context,
		turnContext *persistent.TurnContext,
		messages []*underlying.Message,
	) error

	LoadMessages(ctx context.Context, conversationID string) ([]*underlying.Message, error)

	// Close releases the underlying database connections.
	Close() error
}

var _ Repository = (*persistent.StoreRef)(nil)
var _ Repository = (*persistent.DiscardStore)(nil)

func NewRepository(db *bun.DB, cfg *database.StoreConfig) (Repository, error) {
	if cfg == nil && db == nil {
		return &persistent.DiscardStore{}, nil
	}

	if db != nil {
		return persistent.NewStoreRef(db, slog.With("component", "persistent"), true), nil
	}

	if cfg.Postgres != nil {
		if p, err := database.NewPostgresStore(cfg.Postgres); err != nil {
			return nil, err
		} else {
			return persistent.NewStoreRef(p.DB(), p.Logger(), false), nil
		}
	}

	if s, err := database.NewSqliteStore(cfg.Sqlite); err != nil {
		return nil, err
	} else {
		return persistent.NewStoreRef(s.DB(), s.Logger(), false), nil
	}
}
