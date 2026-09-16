package openai

import (
	"context"
	"os"
	"testing"

	"github.com/taichu-lang/raven-agents/agent/llm"
)

func TestGen(t *testing.T) {
	p := NewProvider(&llm.ProviderOptions{
		ApiKey:   os.Getenv("OPENAI_APIKEY"),
		Endpoint: os.Getenv("OPENAI_API"),
		Model:    "gpt-4.1-nano",
	})

	iter := p.Gen(context.Background(), []*llm.Message{
		{
			Role: llm.RoleUser,
			Contents: llm.MessageContents{
				llm.NewTextContent("1 + 1 = ?"),
			},
		},
	}, llm.WithStream(false))

	for chunk, err := range iter {
		if err != nil {
			t.Log(err)
		} else {
			t.Logf("%s", chunk)
		}
	}
}

func TestGenStream(t *testing.T) {
	p := NewProvider(&llm.ProviderOptions{
		ApiKey:   os.Getenv("OPENAI_APIKEY"),
		Endpoint: os.Getenv("OPENAI_API"),
		Model:    "gpt-4.1-nano",
	})

	iter := p.Gen(context.Background(), []*llm.Message{
		{
			Role: llm.RoleUser,
			Contents: llm.MessageContents{
				llm.NewTextContent("hi"),
			},
		},
	})

	for chunk, err := range iter {
		if err != nil {
			t.Log(err)
		} else {
			t.Logf("%s", chunk)
		}
	}
}
