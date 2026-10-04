package agent

import (
	"context"
	"uuid"

	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/tool"
)

type CtxKeyMessageID struct{}

func WithAssistantMessageID(ctx context.Context) (string, context.Context) {
	v, ok := ctx.Value(CtxKeyMessageID{}).(string)
	if ok {
		return v, ctx
	}

	v = uuid.New().String()
	ctx = context.WithValue(ctx, CtxKeyMessageID{}, v)
	return v, ctx
}

type ContextManager struct {
}

func (c *ContextManager) Build(state *AgentState, tools []tool.FuncTool) ([]*underlying.Message, error) {
	return state.Messages, nil
}
