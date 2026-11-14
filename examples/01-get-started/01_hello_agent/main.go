package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/taichu-lang/raven-agents/agent"
	"github.com/taichu-lang/raven-agents/agent/llm"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/pkg/observability"
)

func main() {
	agent.UseJsonLog(agent.WithLoggerLevel("debug"))
	ctx := context.Background()
	tp, _ := observability.NewTracerProvider(ctx)
	defer tp.Shutdown(ctx)

	model := llm.NewRunner(&underlying.ProviderOptions{
		ApiKey:       os.Getenv("OPENAI_APIKEY"),
		ChatURL:      fmt.Sprintf("%s/v1/responses", os.Getenv("OPENAI_API")),
		Instructions: "You are a helpful assistant!",
		Catalog: &underlying.Catalog{
			ID: "gpt-4.1",
		},
	})

	for chunk, err := range model.RunText(ctx, "Hi") {
		if err != nil {
			slog.Error("something wrong", "err", err)
			return
		}

		for range chunk.Contents {
		}
	}
}
