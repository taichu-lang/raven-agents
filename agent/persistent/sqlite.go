package persistent

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/taichu-lang/raven-agents/migrations"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/sqliteshim"
)

// memoryDSN keeps an in-memory database alive and shared between the pooled connections, so that
// every connection sees the same tables.
const memoryDSN = "file::memory:?cache=shared"

type SqliteConfig struct {
	// Path is the database file. When empty, an in-memory database is used, which is wiped when the
	// process exits.
	Path string `json:"path" yaml:"path"`
}

type SqliteStore struct {
	store
}

func NewSqliteStore(cfg *SqliteConfig) (*SqliteStore, error) {
	set, err := migrations.Sqlite()
	if err != nil {
		return nil, err
	}

	dsn := memoryDSN
	if cfg.Path != "" {
		dsn = fileDSN(cfg.Path)
	}

	sqldb, err := sql.Open(sqliteshim.ShimName, dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", dsn, err)
	}

	// SQLite serializes writes anyway, and an in-memory database is dropped as soon as its last
	// connection is closed, so keep a single long lived connection.
	sqldb.SetMaxOpenConns(1)
	sqldb.SetMaxIdleConns(1)
	sqldb.SetConnMaxLifetime(0)

	s := &SqliteStore{
		db:         bun.NewDB(sqldb, sqlitedialect.New()),
		migrations: set,
		logger:     slog.With("store", "sqlite"),
	}

	if err := s.Migrate(context.Background()); err != nil {
		_ = s.Close()
		return nil, err
	}

	return s, nil
}

// fileDSN builds a file DSN with the pragmas that make concurrent access usable: WAL journaling
// and a busy timeout instead of an immediate "database is locked" error.
func fileDSN(path string) string {
	if strings.Contains(path, "?") {
		// The caller already passed a DSN with its own pragmas, leave it untouched.
		return path
	}

	return "file:" + url.PathEscape(path) +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
}
