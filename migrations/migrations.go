// Package migrations embeds the SQL schema migrations and exposes them as bun migration sets.
//
// The schema is kept per dialect, because the DDL is not portable: Postgres uses BIGSERIAL and
// JSONB, while SQLite uses INTEGER PRIMARY KEY AUTOINCREMENT and TEXT.
package migrations

import (
	"embed"
	"fmt"
	"io/fs"

	"github.com/uptrace/bun/migrate"
)

//go:embed postgres/*.sql
var postgresFS embed.FS

//go:embed sqlite/*.sql
var sqliteFS embed.FS

// Postgres returns the migration set for PostgreSQL.
func Postgres() (*migrate.Migrations, error) {
	return discover(postgresFS)
}

// Sqlite returns the migration set for SQLite.
func Sqlite() (*migrate.Migrations, error) {
	return discover(sqliteFS)
}

func discover(fsys fs.FS) (*migrate.Migrations, error) {
	set := migrate.NewMigrations()
	if err := set.Discover(fsys); err != nil {
		return nil, fmt.Errorf("discover migrations: %w", err)
	}

	return set, nil
}
