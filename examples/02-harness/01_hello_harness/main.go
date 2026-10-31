package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/taichu-lang/raven-agents/agent"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/internal/event"
	"github.com/taichu-lang/raven-agents/internal/observability"
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
			ApiKey:       os.Getenv("OPENAI_APIKEY"),
			Endpoint:     os.Getenv("OPENAI_API"),
			Instructions: "You are a helpful assistant!",
			Model:        "gpt-4.1-nano",
		},
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
