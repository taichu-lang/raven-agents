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
		Name: "hello-world",
	}, &llm.ProviderOptions{
		ApiKey:       os.Getenv("OPENAI_APIKEY"),
		Endpoint:     os.Getenv("OPENAI_API"),
		Instructions: "You are a helpful assistant!",
		Model:        "gpt-4.1-nano",
	})

	for chunk, err := range a.RunText(context.Background(), "Hi") {
		if err != nil {
			slog.Error("something wrong", "err", err)
			return
		}

		for range chunk.Contents {
		}
	}
}
