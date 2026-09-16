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
		Name: "analyze-image",
	}, &llm.ProviderOptions{
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

	for chunk, err := range a.Run(
		context.Background(),
		llm.NewTextContent("what's in this image?"),
		llm.NewDataContent(llm.MediaTypeImageJPG, imageData),
	) {
		if err != nil {
			slog.Error("something wrong", "err", err)
			return
		}

		for range chunk.Contents {
		}
	}
}
