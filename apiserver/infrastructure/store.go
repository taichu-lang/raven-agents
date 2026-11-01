package infrastructure

import (
	"log/slog"

	"github.com/taichu-lang/raven-agents/apiserver/infrastructure/conversation"
	"github.com/taichu-lang/raven-agents/apiserver/infrastructure/skill"
	"github.com/taichu-lang/raven-agents/pkg/database"
	"github.com/uptrace/bun"
)

type Store struct {
	db     *bun.DB
	logger *slog.Logger

	Conversation *conversation.ConversationStore
	Skill        *skill.SkillStore
}

func NewStore(cfg *database.StoreConfig) *Store {
	var db *bun.DB
	var logger *slog.Logger
	if err := cfg.Validate(); err != nil {
		panic(err)
	}

	if cfg.Postgres != nil {
		if p, err := database.NewPostgresStore(cfg.Postgres); err != nil {
			panic(err)
		} else {
			db = p.DB()
			logger = p.Logger()
		}
	} else {
		if s, err := database.NewSqliteStore(cfg.Sqlite); err != nil {
			panic(err)
		} else {
			db = s.DB()
			logger = s.Logger()
		}
	}

	return &Store{
		db:           db,
		logger:       logger,
		Conversation: conversation.NewConversationStore(db, logger),
		Skill:        skill.NewSkillStore(db, logger),
	}
}

// DB exposes the underlying bun database, so that downstream modules can run their own
// queries and schema migrations on the same connection.
func (s *Store) DB() *bun.DB {
	return s.db
}

func (s *Store) Logger() *slog.Logger {
	return s.logger
}

func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}

	return nil
}
