package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/taichu-lang/raven-agents/agent"
	"github.com/taichu-lang/raven-agents/agent/llm"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
)

func main() {
	agent.UseJsonLog(agent.WithLoggerLevel("debug"))

	model := llm.NewRunner(&underlying.ProviderOptions{
		ApiKey:       os.Getenv("OPENAI_APIKEY"),
		ChatURL:      fmt.Sprintf("%s/v1/responses", os.Getenv("OPENAI_API")),
		Instructions: "You are a helpful assistant!",
		Catalog: &underlying.Catalog{
			ID: "gpt-4.1",
		},
	})

	messages := make([]*underlying.Message, 0, 1)
	messages = append(messages, &underlying.Message{
		ID:   "0",
		Role: underlying.RoleUser,
		Contents: underlying.MessageContents{
			underlying.NewTextContent("I am leo"),
		},
	})

	for chunk, err := range model.Run(context.Background(), messages) {
		if err != nil {
			slog.Error("something wrong", "err", err)
			return
		}

		if chunk.Type == underlying.ResponseChunkTypeFinal {
			messages = append(messages, &underlying.Message{
				ID:       chunk.ID,
				Role:     chunk.Role,
				Contents: chunk.Contents,
			})
		}
	}

	messages = append(messages, &underlying.Message{
		ID:   "1",
		Role: underlying.RoleUser,
		Contents: underlying.MessageContents{
			underlying.NewTextContent("Who am i?"),
		},
	})

	for chunk, err := range model.Run(context.Background(), messages) {
		if err != nil {
			slog.Error("something wrong", "err", err)
			return
		}

		for range chunk.Contents {
		}
	}
}
