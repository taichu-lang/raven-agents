package database

import (
	"encoding/json"
	"fmt"

	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
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

	// TurnSeq is the index of the conversation turn that produced the message.
	TurnSeq int64 `bun:"turn_seq,notnull"`

	Role string `bun:"role,notnull"`

	// Content is the JSON encoded underlying.MessageContents of the message.
	Content json.RawMessage `bun:"content,notnull"`

	// Model is the LLM model that produced the message, empty for user and tool messages.
	Model string `bun:"model,notnull"`

	Metadata json.RawMessage `bun:"metadata"`

	// CreatedAt is a unix timestamp in seconds, assigned by the application layer.
	CreatedAt int64 `bun:"created_at,notnull"`
}

func FromMessageRows(rows []MessageModel) ([]*underlying.Message, error) {
	messages := make([]*underlying.Message, 0, len(rows))
	for _, row := range rows {
		contents, err := underlying.UnmarshalMessageContents(row.Content)
		if err != nil {
			return nil, fmt.Errorf("load message %s: %w", row.MessageID, err)
		}

		messages = append(messages, &underlying.Message{
			ID:       row.MessageID,
			Role:     underlying.Role(row.Role),
			Contents: contents,
		})
	}

	return messages, nil
}
