package domain

import "encoding/json"

type Conversation struct {
	ID             int64           `json:"id"`
	ConversationID string          `json:"conversation_id"`
	Title          string          `json:"title"`
	Metadata       json.RawMessage `json:"metadata"`
}
