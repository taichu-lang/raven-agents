package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/taichu-lang/raven-agents/agent"
	"github.com/taichu-lang/raven-agents/agent/llm"
	"github.com/taichu-lang/raven-agents/tool"
	"github.com/taichu-lang/raven-agents/tool/functool"
)

type ReportRequest struct {
	Location    string `json:"location"`
	Weather     string `json:"weather"`
	Temperature string `json:"temperature"`
}

func main() {
	agent.UseJsonLog(agent.WithLoggerLevel("debug"))

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
		return fmt.Sprintf("Based on the weather in %s, I recommend you to stay home.", req.Location), nil
	})

	if err != nil {
		slog.Error("failed to create function tool", "err", err)
		return
	}

	a := agent.NewAgent(&agent.Config{
		Name:  "function-call",
		Tools: []tool.Tool{getWeather, travel},
	}, &llm.ProviderOptions{
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
	})

	for chunk, err := range a.RunText(context.Background(), "i want to travel to Shanghai") {
		if err != nil {
			slog.Error("something wrong", "err", err)
			return
		}

		for range chunk.Contents {
		}
	}
}
