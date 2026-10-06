package agent

import (
	"context"

	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/agent/persistent"
	"github.com/taichu-lang/raven-agents/internal/database"
)

type Repository interface {
	// StoreMessages persists the messages produced by a single conversation turn. Scope carries the
	// conversation attributes that an underlying.Message does not know about.
	StoreMessages(ctx context.Context, model string, messages []*underlying.Message) error

	// Close releases the underlying database connections.
	Close() error
}

var _ Repository = (*persistent.StoreRef)(nil)
var _ Repository = (*persistent.DiscardStore)(nil)

func NewRepository(cfg *database.StoreConfig) (Repository, error) {
	if cfg == nil {
		return &persistent.DiscardStore{}, nil
	}

	if cfg.Postgres != nil {
		if p, err := database.NewPostgresStore(cfg.Postgres); err != nil {
			return nil, err
		} else {
			return persistent.NewStoreRef(p.DB(), p.Logger()), nil
		}
	}

	if s, err := database.NewSqliteStore(cfg.Sqlite); err != nil {
		return nil, err
	} else {
		return persistent.NewStoreRef(s.DB(), s.Logger()), nil
	}
}
