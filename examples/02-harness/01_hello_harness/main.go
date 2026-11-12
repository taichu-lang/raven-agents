package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/taichu-lang/raven-agents/agent"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/pkg/event"
	"github.com/taichu-lang/raven-agents/pkg/observability"
	"github.com/taichu-lang/raven-agents/tool"
)

func main() {
	agent.UseJsonLog(agent.WithLoggerLevel("debug"))
	ctx := context.Background()
	tp, _ := observability.NewTracerProvider(ctx)
	defer tp.Shutdown(ctx)

	bus := event.NewMemoryBus()

	subscription := bus.Subscribe()
	defer bus.Unsubscribe(subscription.ID)

	done := make(chan struct{}, 1)

	go func() {
		for e := range subscription.Channel {
			slog.Debug("on event", "source", e.Source, "name", e.Name, "payload", e.Payload)
			if e.Name == event.EventLLMComplete {
				done <- struct{}{}
			}
		}
	}()

	a := agent.New(bus, tool.NewToolRegistry(nil), &agent.AgentOptions{
		Name: "hello-harness",
		LLM: &underlying.ProviderOptions{
			ApiKey:       os.Getenv("RAVEN_APIKEY"),
			TextURL:      os.Getenv("RAVEN_API"),
			Instructions: "You are a helpful assistant!",
			Catalog: &underlying.Catalog{
				ID: "qwen3.8-flash",
			},
		},
		UserID:         "1",
		ConversationID: "1",
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
