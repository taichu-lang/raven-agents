package application

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"uuid"

	"github.com/taichu-lang/raven-agents/agent"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/agent/persistent"
	"github.com/taichu-lang/raven-agents/agent/skill"
	"github.com/taichu-lang/raven-agents/apiserver/domain"
	"github.com/taichu-lang/raven-agents/apiserver/infrastructure/configs"
	"github.com/taichu-lang/raven-agents/apiserver/infrastructure/conversation"
	"github.com/taichu-lang/raven-agents/internal/event"
)

type ChatService struct {
	cfg       *configs.Config
	store     *conversation.ConversationStore
	skillsDir string
}

func NewChatService(
	cfg *configs.Config,
	store *conversation.ConversationStore,
) *ChatService {
	return &ChatService{
		cfg:       cfg,
		store:     store,
		skillsDir: path.Join(cfg.AppDir, ".agents", "skills"),
	}
}

// Chat runs one turn of the conversation in the background and returns the stream of events it
// produces. The channel is closed once the turn is over, so the caller can simply range over it.
//
// The caller must keep receiving until the channel is closed, or cancel ctx. Both unblock the agent:
// the bus pushes back on a producer whose subscriber stopped reading, and only a cancelled ctx lets
// that producer give up.
func (s *ChatService) Chat(
	ctx context.Context,
	req *domain.ChatRequest,
) (<-chan *event.Event, error) {
	c, err := s.store.Get(ctx, req.ConversationID)
	if err != nil {
		return nil, fmt.Errorf("database error: %w", err)
	}

	if c == nil {
		c = &conversation.ConversationModel{
			ConversationID: req.ConversationID,
			UserID:         1,
			Title:          "",
		}
		if err := s.store.Create(ctx, c); err != nil {
			return nil, fmt.Errorf("database error: %w", err)
		}
	}

	bus := event.NewMemoryBus()
	subscription := bus.Subscribe()

	a := agent.New(bus, agent.NewToolRegistry(), &agent.AgentOptions{
		Name:  "some-name",
		Store: s.cfg.Store,
		LLM: &underlying.ProviderOptions{
			ApiKey:       s.cfg.ApiKey,
			Endpoint:     s.cfg.Endpoint,
			Instructions: s.cfg.Instructions,
			Model:        req.Model,
		},
		SkillDirs: []string{s.skillsDir},
	})

	skills := s.resolveSkills(req.Metadata.Skills)
	go func() {
		// Unsubscribing closes the channel, which ends the consumer's receive loop.
		defer bus.Unsubscribe(subscription.ID)
		messages := make([]*underlying.Message, 0, 1+len(skills))
		messages = append(messages, &underlying.Message{
			ID:   uuid.New().String(),
			Role: underlying.RoleUser,
			Contents: underlying.MessageContents{
				underlying.NewTextContent(req.Text),
			},
		})
		for _, skill := range skills {
			messages = append(messages, &underlying.Message{
				ID:   uuid.New().String(),
				Role: underlying.RoleUser,
				Contents: underlying.MessageContents{
					underlying.NewUnresolvedSkillContent(skill),
				},
			})
		}

		if err := a.Run(persistent.WithTurnContext(ctx, &persistent.TurnContext{
			UserID:         "1",
			ConversationID: req.ConversationID,
			Turn:           1,
		}), messages); err != nil {
			// Report the failure down the stream so that it keeps its place in the order of the events the
			// consumer has already seen. ctx is detached because the failure is usually the cancellation
			// itself, and the event would then be dropped by the bus.
			bus.Emit(context.WithoutCancel(ctx), &event.Event{
				ID:      uuid.New().String(),
				Source:  event.SourceTypeRuntime,
				Name:    event.EventRuntimeError,
				Payload: err,
			})
		}

		// Shutdown still has to drain the tracer and close the store, which ctx being already cancelled
		// would skip.
		if err := a.Shutdown(context.WithoutCancel(ctx)); err != nil {
			slog.Warn("failed to shutdown agent", "conversation_id", req.ConversationID, "err", err)
		}
	}()

	return subscription.Channel, nil
}

func (s *ChatService) resolveSkills(skills []skill.Skill) []string {
	names := make([]string, 0, len(skills))
	for _, selected := range skills {
		names = append(names, selected.Name)
	}

	return names
}
