package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/taichu-lang/raven-agents/agent"
	"github.com/taichu-lang/raven-agents/agent/llm"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
)

func main() {
	agent.UseJsonLog(agent.WithLoggerLevel("debug"))

	model := llm.NewRunner(&underlying.ProviderOptions{
		ApiKey:   os.Getenv("OPENAI_APIKEY"),
		Endpoint: os.Getenv("OPENAI_API"),
		Model:    "gpt-6-astra",
	})

	image := "<your_image_file>"
	imageData, err := os.ReadFile(image)
	if err != nil {
		slog.Error("failed to load image", "err", err)
		return
	}

	for chunk, err := range model.Run(
		context.Background(),
		[]*underlying.Message{
			{
				ID:   "0",
				Role: underlying.RoleUser,
				Contents: underlying.MessageContents{
					underlying.NewTextContent("what's in this image?"),
				},
			},
			{
				ID:   "1",
				Role: underlying.RoleUser,
				Contents: underlying.MessageContents{
					underlying.NewDataContent(underlying.MediaTypeImageJPG, imageData),
				},
			},
		},
	) {
		if err != nil {
			slog.Error("something wrong", "err", err)
			return
		}

		for range chunk.Contents {
		}
	}
}
