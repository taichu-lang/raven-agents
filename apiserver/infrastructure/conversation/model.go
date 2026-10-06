package conversation

import (
	"encoding/json"

	"github.com/uptrace/bun"
)

// ConversationModel maps a single conversation onto the `conversations` table. Keep it in sync with
// migrations/postgres/02_conversations.up.sql and migrations/sqlite/02_conversations.up.sql.
type ConversationModel struct {
	bun.BaseModel `bun:"table:conversations,alias:c"`

	ID int64 `bun:"id,pk,autoincrement"`

	// ConversationID is the public identifier of the conversation, the one carried by the messages
	// of the conversation. It is unique so that it can be used as the lookup key.
	ConversationID string `bun:"conversation_id,notnull,unique"`

	// UserID is the owner of the conversation.
	UserID int64 `bun:"user_id,notnull"`

	Title string `bun:"title,notnull"`

	Metadata json.RawMessage `bun:"metadata"`

	// CreatedAt is a unix timestamp in seconds, assigned by the application layer.
	CreatedAt int64 `bun:"created_at,notnull"`
}
