package domain

import "encoding/json"

type Conversation struct {
	ID             int64           `json:"id"`
	ConversationID string          `json:"conversation_id"`
	Title          string          `json:"title"`
	Metadata       json.RawMessage `json:"metadata"`
	CreatedAt      int64           `json:"created_at"`
}

type ConversationPageRequest struct {
	From  int64 `form:"from"`
	Limit int64 `form:"limit"`
}
