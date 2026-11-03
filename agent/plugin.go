package agent

import (
	"context"

	"github.com/taichu-lang/raven-agents/agent/llm"
)

// MiddlewareSource is the plugin point for injecting LLM middlewares into the agent's execution flow from
// outside the agent package, ex: an apiserver customization layering in request-scoped, cross-cutting concerns
// such as metering, budget enforcement or tracing. Sources are installed on the request context by an upstream
// HTTP middleware via ContextWithMiddlewareSource, so the chat service itself stays unaware of what is plugged
// in.
type MiddlewareSource interface {
	// WithMiddlewares builds the middlewares this source contributes for the request described by mc. Returning
	// nil contributes nothing.
	WithMiddlewares(ctx context.Context, mc *MiddlewareContext) []llm.Middleware
}

// MiddlewareContext carries the request-scoped identity a source needs to build its middlewares.
type MiddlewareContext struct {
	UserID         int64
	ConversationID string
	Model          string
}

type agentMiddlewareSourcesKey struct{}

// ContextWithMiddlewareSource appends a source to the context.
func ContextWithMiddlewareSource(ctx context.Context, source MiddlewareSource) context.Context {
	sources := append(MiddlewareSourcesFromContext(ctx), source)
	return context.WithValue(ctx, agentMiddlewareSourcesKey{}, sources)
}

func MiddlewareSourcesFromContext(ctx context.Context) []MiddlewareSource {
	sources, _ := ctx.Value(agentMiddlewareSourcesKey{}).([]MiddlewareSource)
	return sources
}
