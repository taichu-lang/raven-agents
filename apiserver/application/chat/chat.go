package chat

import (
	"context"

	"github.com/google/uuid"
	"github.com/taichu-lang/raven-agents/agent"
	"github.com/taichu-lang/raven-agents/agent/llm"
	chatdomain "github.com/taichu-lang/raven-agents/apiserver/domain/chat"
	"github.com/taichu-lang/raven-agents/apiserver/infrastructure/configs"
)

type Service struct {
	agent *agent.Agent
}

func NewService(cfg *configs.Config) *Service {
	return &Service{
		agent: agent.NewAgent(&agent.Config{
			Name: "chat",
		}, &llm.ProviderOptions{
			ApiKey:       cfg.ApiKey,
			Endpoint:     cfg.Endpoint,
			Model:        "gpt-4.1",
			Instructions: cfg.Instructions,
		}),
	}
}

func (s *Service) Chat(
	ctx context.Context,
	req *chatdomain.Request,
) (*chatdomain.MessageMetadata, llm.ResponseStream) {
	messageID := uuid.New().String()
	user := &llm.Message{
		ID:   messageID,
		Role: llm.RoleUser,
		Contents: llm.MessageContents{
			llm.NewTextContent(req.Text),
		},
	}

	return &chatdomain.MessageMetadata{ID: messageID}, s.agent.Run(ctx, []*llm.Message{user})
}
