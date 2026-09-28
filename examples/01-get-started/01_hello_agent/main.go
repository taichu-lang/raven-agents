package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/taichu-lang/raven-agents/agent"
	"github.com/taichu-lang/raven-agents/agent/llm"
	"github.com/taichu-lang/raven-agents/internal/observability"
)

func main() {
	agent.UseJsonLog(agent.WithLoggerLevel("debug"))
	ctx := context.Background()
	tp, _ := observability.NewTracerProvider(ctx)
	defer tp.Shutdown(ctx)

	a := agent.NewAgent(&agent.Config{
		Name: "hello-world",
	}, &llm.ProviderOptions{
		ApiKey:       os.Getenv("OPENAI_APIKEY"),
		Endpoint:     os.Getenv("OPENAI_API"),
		Instructions: "You are a helpful assistant!",
		Model:        "gpt-4.1-nano",
	})

	for chunk, err := range a.RunText(ctx, "Hi") {
		if err != nil {
			slog.Error("something wrong", "err", err)
			return
		}

		for range chunk.Contents {
		}
	}
}
