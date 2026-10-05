package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"

	"github.com/taichu-lang/raven-agents/agent"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/internal/event"
	"github.com/taichu-lang/raven-agents/internal/observability"
	"github.com/taichu-lang/raven-agents/tool/functool"
)

type ReportRequest struct {
	Location    string `json:"location"`
	Weather     string `json:"weather"`
	Temperature string `json:"temperature"`
}

func main() {
	agent.UseJsonLog(agent.WithLoggerLevel("debug"))
	ctx := context.Background()
	tp, _ := observability.NewTracerProvider(ctx)
	defer tp.Shutdown(ctx)

	var travelFuncCalled atomic.Bool

	getWeather, err := functool.New(functool.Config{
		Name:        "weather",
		Description: "Get the current weather for a given location",
	}, func(ctx context.Context, location string) (string, error) {
		return fmt.Sprintf("The weather in %s is cloudy, 20°C.", location), nil
	})

	if err != nil {
		slog.Error("failed to create function tool", "err", err)
		return
	}

	travel, err := functool.New(functool.Config{
		Name:        "travel_planning",
		Description: "Based on local weather conditions, create a travel plan. The weather info should get from weather function tool.",
	}, func(ctx context.Context, req ReportRequest) (string, error) {
		travelFuncCalled.Store(true)
		return fmt.Sprintf("Based on the weather in %s, I recommend you to stay home.", req.Location), nil
	})

	if err != nil {
		slog.Error("failed to create function tool", "err", err)
		return
	}

	registry := agent.NewToolRegistry()
	registry.Register(getWeather, nil)
	registry.Register(travel, nil)

	bus := event.NewMemoryBus()
	subscription := bus.Subscribe()
	defer bus.Unsubscribe(subscription.ID)

	done := make(chan struct{}, 1)
	go func() {
		for e := range subscription.Channel {
			slog.Debug("on event", "source", e.Source, "name", e.Name, "payload", e.Payload)
			if e.Name == event.EventLLMComplete && travelFuncCalled.Load() {
				done <- struct{}{}
			}
		}
	}()

	a := agent.New(bus, registry, &agent.AgentOptions{
		Name: "harness-function",
		LLM: &underlying.ProviderOptions{
			ApiKey:   os.Getenv("OPENAI_APIKEY"),
			Endpoint: os.Getenv("OPENAI_API"),
			Instructions: `
		You are a travel assistant. When users ask travel-related questions, please follow this workflow:

1. First, call get_weather to obtain weather information for the destination.
2. Based on the returned temperature and weather data, then call travel_planning to create a travel plan.
3. Do not call travel_planning directly without first obtaining weather data.

Important: Only call the tool needed at each step, and wait for the result before deciding on the next action.
		`,
			Model: "gpt-4.1-nano",
		},
	})
	a.Run(ctx, []*underlying.Message{
		{
			ID:   "1",
			Role: underlying.RoleUser,
			Contents: underlying.MessageContents{
				underlying.NewTextContent("i want to travel to Shanghai"),
			},
		},
	})
	<-done

	a.Shutdown(ctx)
}
