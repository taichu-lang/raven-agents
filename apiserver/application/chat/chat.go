package chat

import (
	"context"

	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	chatdomain "github.com/taichu-lang/raven-agents/apiserver/domain/chat"
	"github.com/taichu-lang/raven-agents/apiserver/infrastructure/configs"
)

type Service struct {
}

func NewService(cfg *configs.Config) *Service {
	return &Service{}
}

func (s *Service) Chat(
	ctx context.Context,
	req *chatdomain.Request,
) (*chatdomain.MessageMetadata, underlying.ResponseStream) {
	return nil, nil
}
