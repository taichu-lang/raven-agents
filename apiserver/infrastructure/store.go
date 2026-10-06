package infrastructure

import (
	"log/slog"

	"github.com/taichu-lang/raven-agents/apiserver/infrastructure/conversation"
	"github.com/taichu-lang/raven-agents/apiserver/infrastructure/user"
	"github.com/taichu-lang/raven-agents/internal/database"
	"github.com/uptrace/bun"
)

type Store struct {
	db *bun.DB

	User         *user.UserStore
	Conversation *conversation.ConversationStore
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
		User:         user.NewUserStore(db, logger),
		Conversation: conversation.NewConversationStore(db, logger),
	}
}

func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}

	return nil
}
