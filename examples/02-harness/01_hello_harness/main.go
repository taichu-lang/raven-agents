package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/taichu-lang/raven-agents/agent"
	"github.com/taichu-lang/raven-agents/agent/llm"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/internal/event"
	"github.com/taichu-lang/raven-agents/internal/observability"
)

func main() {
	agent.UseJsonLog(agent.WithLoggerLevel("debug"))
	ctx := context.Background()
	tp, _ := observability.NewTracerProvider(ctx)
	defer tp.Shutdown(ctx)

	model := llm.NewRunner(&underlying.ProviderOptions{
		ApiKey:       os.Getenv("OPENAI_APIKEY"),
		Endpoint:     os.Getenv("OPENAI_API"),
		Instructions: "You are a helpful assistant!",
		Model:        "gpt-4.1-nano",
	})

	bus := event.NewMemoryBus()
	handle, _ := bus.Subscribe(func(event *event.Event) {
		slog.Debug("on event", "source", event.Source, "name", event.Name, "payload", event.Payload)
	})
	defer bus.Unsubscribe(handle)

	a := agent.New(bus, model, agent.NewToolRegistry())
	a.Run(context.Background(), "gpt-4.1-nano", []*underlying.Message{
		{
			ID:   "1",
			Role: underlying.RoleUser,
			Contents: underlying.MessageContents{
				underlying.NewTextContent("hi"),
			},
		},
	})
}
