package domain

import (
	"encoding/json"

	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/apiserver/infrastructure/conversation"
)

type Conversation struct {
	ID             int64           `json:"id"`
	ConversationID string          `json:"conversation_id"`
	Title          string          `json:"title"`
	Metadata       json.RawMessage `json:"metadata"`
	CreatedAt      int64           `json:"created_at"`
}

type ConversationView struct {
	Conversation
	Messages []*underlying.Message `json:"messages"`
}

type ConversationPageRequest struct {
	From  int64 `form:"from"`
	Limit int64 `form:"limit"`
}

func ConversationFromModel(model *conversation.ConversationModel) Conversation {
	return Conversation{
		ID:             model.ID,
		ConversationID: model.ConversationID,
		Title:          model.Title,
		Metadata:       model.Metadata,
		CreatedAt:      model.CreatedAt,
	}
}
