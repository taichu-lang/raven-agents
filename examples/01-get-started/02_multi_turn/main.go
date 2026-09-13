package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/taichu-lang/raven-agents/agent"
	"github.com/taichu-lang/raven-agents/agent/llm"
)

func main() {
	agent.UseJsonLog(agent.WithLoggerLevel("debug"))

	a := agent.NewAgent(&agent.Config{
		Instructions: "You are a helpful assistant!",
		Model:        "gpt-4.1-nano",
	}, &llm.ProviderOptions{
		ApiKey:   os.Getenv("OPENAI_APIKEY"),
		Endpoint: os.Getenv("OPENAI_API"),
	})

	for chunk, err := range a.RunText(context.Background(), "I am leo") {
		if err != nil {
			slog.Error("something wrong", "err", err)
			return
		}

		for range chunk.Contents {
		}
	}

	for chunk, err := range a.RunText(context.Background(), "Who am i?") {
		if err != nil {
			slog.Error("something wrong", "err", err)
			return
		}

		for range chunk.Contents {
		}
	}
}
