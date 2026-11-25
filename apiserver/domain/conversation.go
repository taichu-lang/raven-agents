package domain

import (
	"encoding/json"

	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/apiserver/infrastructure"
	"github.com/taichu-lang/raven-agents/pkg/database"
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
	Turns []*Turn `json:"turns"`
}

type Turn struct {
	ID             int64                 `json:"id"`
	ConversationID string                `json:"conversation_id"`
	TurnSeq        int64                 `json:"turn_seq"`
	Status         database.TurnStatus   `json:"status"`
	TokensUsed     int64                 `json:"tokens_used"`
	Iterations     int64                 `json:"iterations"`
	UserMessageID  string                `json:"user_message_id"`
	CreatedAt      int64                 `json:"created_at"`
	UpdatedAt      int64                 `json:"updated_at"`
	Messages       []*underlying.Message `json:"messages"`
	Annotations    json.RawMessage       `json:"annotations"`
}

type ConversationPageRequest struct {
	From  int64 `form:"from"`
	Limit int64 `form:"limit"`
}

func TurnsFromModels(
	turns []*database.TurnModel,
	messagesEachTurn map[int64][]*underlying.Message,
) []*Turn {
	views := make([]*Turn, 0, len(turns))
	for _, t := range turns {
		messages := messagesEachTurn[t.TurnSeq]
		if messages == nil {
			messages = []*underlying.Message{}
		}

		turn := TurnFromModel(t)
		turn.Messages = messages
		views = append(views, turn)
	}

	return views
}

func ConversationFromModel(model *infrastructure.ConversationModel) Conversation {
	return Conversation{
		ID:             model.ID,
		ConversationID: model.ConversationID,
		Title:          model.Title,
		Metadata:       model.Metadata,
		CreatedAt:      model.CreatedAt,
	}
}

func TurnFromModel(t *database.TurnModel) *Turn {
	return &Turn{
		ID:             t.ID,
		ConversationID: t.ConversationID,
		TurnSeq:        t.TurnSeq,
		Status:         t.Status,
		TokensUsed:     t.TokensUsed,
		Iterations:     t.Iterations,
		UserMessageID:  t.UserMessageID,
		CreatedAt:      t.CreatedAt,
		UpdatedAt:      t.UpdatedAt,
		Annotations:    t.Annotations,
	}
}
