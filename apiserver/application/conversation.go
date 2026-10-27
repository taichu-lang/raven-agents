package application

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/taichu-lang/raven-agents/agent/llm"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/apiserver/domain"
	"github.com/taichu-lang/raven-agents/apiserver/infrastructure/conversation"
	"github.com/taichu-lang/raven-agents/internal/database"
	"github.com/taichu-lang/raven-agents/internal/event"
)

type TitleGeneration struct {
	done sync.WaitGroup
}

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

func (s *ChatService) genConversationTitle(
	ctx context.Context,
	req *domain.ChatRequest,
	c *conversation.ConversationModel,
	bus event.Bus,
) *TitleGeneration {
	gen := &TitleGeneration{}
	if c.Title == "" {
		gen.done.Add(1)
		runner := llm.NewRunner(&underlying.ProviderOptions{
			ApiKey:   s.cfg.ApiKey,
			Endpoint: s.cfg.Endpoint,
			Model:    req.Model,
		})

		var prompt bytes.Buffer
		if err := s.promptTemplate.Execute(&prompt, map[string]interface{}{
			"UserInput": req.Text,
		}); err != nil {
			s.logger.Warn("failed to build prompt for title generation", "err", err)
			gen.done.Done()
			return gen
		}

		go func() {
			title := gen.generate(ctx, runner, prompt.String())
			if title != "" {
				if err := s.store.UpdateTitle(ctx, c.ID, title); err != nil {
					s.logger.Warn("database error", "err", err)
				}

				bus.Emit(ctx, &event.Event{
					ID:     uuid.NewString(),
					Source: event.SourceTypeAgent,
					Name:   EventConversationStart,
					Payload: &domain.Conversation{
						ID:             c.ID,
						ConversationID: c.ConversationID,
						Title:          title,
					},
				})
			}

			gen.done.Done()
		}()
	}

	return gen
}

func (g *TitleGeneration) generate(ctx context.Context, runner *llm.Runner, input string) string {
	var title string
	for chunk, err := range runner.Run(ctx, []*underlying.Message{
		{
			ID:   uuid.NewString(),
			Role: underlying.RoleUser,
			Contents: underlying.MessageContents{
				underlying.NewTextContent(input),
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

func (g *TitleGeneration) wait() {
	g.done.Wait()
}
