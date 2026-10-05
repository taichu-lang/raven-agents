package persistent

import (
	"encoding/json"

	"github.com/uptrace/bun"
)

// MessageModel maps a single LLM message onto the `messages` table. Keep it in sync with
// migrations/01_messages.up.sql.
type MessageModel struct {
	bun.BaseModel `bun:"table:messages,alias:m"`

	ID int64 `bun:"id,pk,autoincrement"`

	// MessageID is the provider assigned message identifier. It is unique so that re-storing the
	// same message (ex: an assistant message replayed across iterations) updates the row in place
	// instead of duplicating it.
	MessageID string `bun:"message_id,notnull,unique"`

	// ConversationID identifies the multi-turn conversation the message belongs to.
	ConversationID string `bun:"conversation_id,notnull"`

	// Turn is the index of the conversation turn that produced the message.
	Turn int64 `bun:"turn,notnull"`

	Role string `bun:"role,notnull"`

	// Content is the JSON encoded underlying.MessageContents of the message.
	Content json.RawMessage `bun:"content,notnull"`

	// Model is the LLM model that produced the message, empty for user and tool messages.
	Model string `bun:"model,notnull"`

	Metadata json.RawMessage `bun:"metadata"`

	// CreatedAt is a unix timestamp in seconds, assigned by the application layer.
	CreatedAt int64 `bun:"created_at,notnull"`
}
