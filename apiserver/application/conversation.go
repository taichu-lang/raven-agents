package application

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/taichu-lang/raven-agents/agent/llm"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/apiserver/domain"
	"github.com/taichu-lang/raven-agents/internal/database"
)

// GetConversations returns one cursor page of the conversations of a user, the most recently
// created first. from is the id of the last item of the previous page, zero starts from the latest.
func (s *ChatService) GetConversations(
	ctx context.Context,
	userID, from, limit int64,
) ([]domain.Conversation, error) {
	if limit <= 0 {
		limit = domain.DefaultPaginationSize
	}

	if from < 0 {
		from = 0
	}

	models, err := s.store.List(ctx, userID, from, limit)
	if err != nil {
		return nil, fmt.Errorf("database error: %w", err)
	}

	items := make([]domain.Conversation, 0, len(models))
	for _, model := range models {
		items = append(items, domain.ConversationFromModel(model))
	}

	return items, nil
}

func (s *ChatService) GetConversation(
	ctx context.Context,
	conversationID string,
) (*domain.ConversationView, error) {
	cm, err := s.store.Get(ctx, conversationID)
	if cm != nil {
		mm, err := s.store.GetMessages(ctx, conversationID)
		if err != nil {
			return nil, err
		}

		messages, err := database.FromMessageRows(mm)
		if err != nil {
			return nil, err
		}

		view := &domain.ConversationView{
			Conversation: domain.ConversationFromModel(cm),
			Messages:     messages,
		}
		return view, nil
	}

	return nil, err
}

func (s *ChatService) genConversationTitle(ctx context.Context, req *domain.ChatRequest) string {
	runner := llm.NewRunner(&underlying.ProviderOptions{
		ApiKey:       s.cfg.ApiKey,
		Endpoint:     s.cfg.Endpoint,
		Instructions: s.cfg.Prompt.TitleGen,
		Model:        req.Model,
	})

	var title string
	for chunk, err := range runner.Run(ctx, []*underlying.Message{
		{
			ID:   uuid.NewString(),
			Role: underlying.RoleUser,
			Contents: underlying.MessageContents{
				underlying.NewTextContent(req.Text),
			},
		},
	}, underlying.WithStream(false)) {
		// Do not return here, i.e., do not break the stream iteration.
		if err == nil && chunk.Type == underlying.ResponseChunkTypeFinal {
			title = chunk.Contents.CollectText()
		}
	}

	return title
}
