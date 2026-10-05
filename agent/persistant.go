package agent

import (
	"context"
	"errors"

	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/agent/persistent"
)

type StoreConfig struct {
	Postgres *persistent.PostgresConfig `json:"postgres" yaml:"postgres"`
	Sqlite   *persistent.SqliteConfig   `json:"sqlite"   yaml:"sqlite"`
}

type Repository interface {
	// StoreMessages persists the messages produced by a single conversation turn. Scope carries the
	// conversation attributes that an underlying.Message does not know about.
	StoreMessages(ctx context.Context, model string, messages []*underlying.Message) error

	// Close releases the underlying database connections.
	Close() error
}

var _ Repository = (*persistent.PostgresStore)(nil)
var _ Repository = (*persistent.SqliteStore)(nil)
var _ Repository = (*persistent.DiscardStore)(nil)

func NewRepository(cfg *StoreConfig) (Repository, error) {
	if cfg == nil {
		return &persistent.DiscardStore{}, nil
	}

	if cfg.Postgres != nil {
		return persistent.NewPostgresStore(cfg.Postgres)
	}

	return persistent.NewSqliteStore(cfg.Sqlite)
}

func (s *StoreConfig) Validate() error {
	if s.Postgres == nil && s.Sqlite == nil {
		return errors.New("one persistent driver is required")
	}

	if s.Postgres != nil {
		if s.Postgres.Address == "" {
			return errors.New("[postgres] address is required")
		}

		if s.Postgres.Database == "" {
			return errors.New("[postgres] database is required")
		}

		if s.Postgres.User == "" || s.Postgres.Password == "" {
			return errors.New("[postgres] auth is required")
		}
	}

	return nil
}
