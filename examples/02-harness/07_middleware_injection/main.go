package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/taichu-lang/raven-agents/agent"
	"github.com/taichu-lang/raven-agents/agent/llm"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/pkg/event"
	"github.com/taichu-lang/raven-agents/pkg/observability"
	"github.com/taichu-lang/raven-agents/tool"
)

type CustomMiddleware struct {
	tokens int64
}

func (c *CustomMiddleware) Run(
	next llm.RunFunc,
	ctx context.Context,
	messages []*underlying.Message,
	options *underlying.GenOptions,
) underlying.ResponseStream {
	return func(yield func(*underlying.ResponseChunk, error) bool) {
		for chunk, err := range next(ctx, messages, options) {
			if err == nil && chunk != nil && chunk.Type == underlying.ResponseChunkTypeUsage {
				c.tokens += chunk.Contents[0].(*underlying.UsageContent).Total
			}

			if !yield(chunk, err) {
				return
			}
		}
	}
}

func main() {
	agent.UseJsonLog(agent.WithLoggerLevel("debug"))
	ctx := context.Background()
	tp, _ := observability.NewTracerProvider(ctx)
	defer tp.Shutdown(ctx)

	bus := event.NewMemoryBus()

	subscription := bus.Subscribe()
	defer bus.Unsubscribe(subscription.ID)

	mc := &CustomMiddleware{}

	done := make(chan struct{}, 1)
	go func() {
		for e := range subscription.Channel {
			slog.Debug("on event", "source", e.Source, "name", e.Name, "payload", e.Payload)
			if e.Name == event.EventLLMComplete {
				if mc.tokens == 0 {
					panic("should not happen")
				}

				done <- struct{}{}
			}
		}
	}()

	a := agent.New(bus, tool.NewToolRegistry(nil), &agent.AgentOptions{
		Name: "middleware-injection",
		LLM: &underlying.ProviderOptions{
			ApiKey:       os.Getenv("OPENAI_APIKEY"),
			ChatURL:      fmt.Sprintf("%s/v1/responses", os.Getenv("OPENAI_API")),
			Instructions: "You are a helpful assistant!",
			Catalog: &underlying.Catalog{
				ID: "gpt-4.1",
			},
		},
		UserID:         "1",
		ConversationID: "1",
		Middlewares:    []llm.Middleware{mc},
	})
	a.Run(ctx, []*underlying.Message{
		{
			ID:   "1",
			Role: underlying.RoleUser,
			Contents: underlying.MessageContents{
				underlying.NewTextContent("hi"),
			},
		},
	})
	<-done

	a.Shutdown(ctx)
}
