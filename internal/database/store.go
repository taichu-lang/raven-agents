package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
)

type StoreConfig struct {
	Postgres *PostgresConfig `json:"postgres" yaml:"postgres"`
	Sqlite   *SqliteConfig   `json:"sqlite"   yaml:"sqlite"`
}

// EmptyJSONObject is the fallback for the NOT NULL json columns.
var EmptyJSONObject = json.RawMessage(`{}`)

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

func (s *store) Logger() *slog.Logger {
	return s.logger
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
