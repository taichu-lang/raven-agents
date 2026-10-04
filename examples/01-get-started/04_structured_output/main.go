package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/taichu-lang/raven-agents/agent"
	"github.com/taichu-lang/raven-agents/agent/llm"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
)

type Entities struct {
	Attributes []string `json:"attributes"`
	Colors     []string `json:"colors"`
	Animals    []string `json:"animals"`
}

func main() {
	agent.UseJsonLog(agent.WithLoggerLevel("debug"))

	model := llm.NewRunner(&underlying.ProviderOptions{
		ApiKey:       os.Getenv("OPENAI_APIKEY"),
		Endpoint:     os.Getenv("OPENAI_API"),
		Model:        "gpt-6-astra",
		Instructions: "Extract entities from the input text",
	})

	for chunk, err := range model.Run(
		context.Background(),
		[]*underlying.Message{
			{
				ID:   "0",
				Role: underlying.RoleUser,
				Contents: underlying.MessageContents{
					underlying.NewTextContent("The quick brown fox jumps over the lazy dog with piercing blue eyes"),
				},
			},
		},
		underlying.WithOutputFormat[Entities](),
	) {
		if err != nil {
			slog.Error("something wrong", "err", err)
			return
		}

		for range chunk.Contents {
		}
	}
}
