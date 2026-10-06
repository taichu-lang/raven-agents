package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/taichu-lang/raven-agents/migrations"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

// postgresPingTimeout bounds the connectivity check done while constructing the store.
const postgresPingTimeout = 5 * time.Second

type PostgresConfig struct {
	User     string `json:"user"     yaml:"user"`
	Password string `json:"password" yaml:"password"`
	Address  string `json:"address"  yaml:"address"`
	Database string `json:"database" yaml:"database"`

	// Insecure disables TLS on the connection. Keep it false for anything but a local database.
	Insecure bool `json:"insecure" yaml:"insecure"`
}

type PostgresStoreRef struct {
	store
}

func NewPostgresStore(cfg *PostgresConfig) (*PostgresStoreRef, error) {
	if cfg == nil {
		return nil, errors.New("persistent: nil postgres config")
	}

	if cfg.Address == "" || cfg.Database == "" {
		return nil, errors.New("persistent: postgres address and database are required")
	}

	set, err := migrations.Postgres()
	if err != nil {
		return nil, err
	}

	connector := pgdriver.NewConnector(
		pgdriver.WithNetwork("tcp"),
		pgdriver.WithAddr(cfg.Address),
		pgdriver.WithUser(cfg.User),
		pgdriver.WithPassword(cfg.Password),
		pgdriver.WithDatabase(cfg.Database),
		pgdriver.WithInsecure(cfg.Insecure),
		pgdriver.WithApplicationName("raven-agents"),
	)

	db := bun.NewDB(sql.OpenDB(connector), pgdialect.New())
	s := &PostgresStoreRef{
		db:         db,
		migrations: set,
		logger:     slog.With("store", "postgres"),
	}

	ctx, cancel := context.WithTimeout(context.Background(), postgresPingTimeout)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect postgres %s/%s: %w", cfg.Address, cfg.Database, err)
	}

	if err := s.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return s, nil
}
