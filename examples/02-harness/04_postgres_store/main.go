package main

import (
	"context"
	"log/slog"
	"os"
	"uuid"

	"github.com/taichu-lang/raven-agents/agent"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/agent/persistent"
	"github.com/taichu-lang/raven-agents/internal/database"
	"github.com/taichu-lang/raven-agents/internal/event"
	"github.com/taichu-lang/raven-agents/internal/observability"
)

func main() {
	agent.UseJsonLog(agent.WithLoggerLevel("debug"))
	ctx := context.Background()
	tp, _ := observability.NewTracerProvider(ctx)
	defer tp.Shutdown(ctx)

	firstReceived := make(chan struct{}, 1)
	done := make(chan struct{}, 1)

	bus := event.NewMemoryBus()
	subscription := bus.Subscribe()
	defer bus.Unsubscribe(subscription.ID)

	go func() {
		count := 0
		for e := range subscription.Channel {
			if e.Name == event.EventLLMComplete {
				count++
				slog.Info("assistant message is accepted", "message", e.Payload)
				if count == 1 {
					firstReceived <- struct{}{}
				}

				if count == 2 {
					done <- struct{}{}
				}
			}
		}
	}()

	a := agent.New(bus, agent.NewToolRegistry(), &agent.AgentOptions{
		Name: "postgres-store",
		LLM: &underlying.ProviderOptions{
			ApiKey:       os.Getenv("OPENAI_APIKEY"),
			Endpoint:     os.Getenv("OPENAI_API"),
			Instructions: "You are a helpful assistant!",
			Model:        "gpt-4.1-nano",
		},
		Store: &database.StoreConfig{
			Postgres: &database.PostgresConfig{
				Address:  "localhost:46873",
				User:     "postgres",
				Password: "postgres",
				Database: "raven",
				Insecure: true,
			},
		},
	})

	if err := a.Run(persistent.WithTurnContext(ctx, &persistent.TurnContext{
		UserID:         "user-0",
		ConversationID: "conv-1",
		Turn:           1,
	}), []*underlying.Message{
		{
			ID:   uuid.New().String(),
			Role: underlying.RoleUser,
			Contents: underlying.MessageContents{
				underlying.NewTextContent("Hi, i am leo"),
			},
		},
	}); err != nil {
		panic(err)
	}

	<-firstReceived
	if err := a.Run(persistent.WithTurnContext(ctx, &persistent.TurnContext{
		UserID:         "user-0",
		ConversationID: "conv-1",
		Turn:           2,
	}), []*underlying.Message{
		{
			ID:   uuid.New().String(),
			Role: underlying.RoleUser,
			Contents: underlying.MessageContents{
				underlying.NewTextContent("Who am i?"),
			},
		},
	}); err != nil {
		panic(err)
	}

	<-done

	a.Shutdown(ctx)
}
