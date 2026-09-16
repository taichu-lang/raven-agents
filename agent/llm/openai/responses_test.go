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
	}, &llm.GenOptions{Stream: false})

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
	}, nil)

	for chunk, err := range iter {
		if err != nil {
			t.Log(err)
		} else {
			t.Logf("%s", chunk)
		}
	}
}

type Step struct {
	Explanation string `json:"explanation" validate:"required"`
	Output      string `json:"output"      validate:"required"`
}

type MathReasoning struct {
	Steps       []Step `json:"steps"        validate:"required"`
	FinalAnswer string `json:"final_answer" validate:"required"`
}

func TestStructuredOutput(t *testing.T) {
	p := NewProvider(&llm.ProviderOptions{
		ApiKey:       os.Getenv("OPENAI_APIKEY"),
		Endpoint:     os.Getenv("OPENAI_API"),
		Model:        "gpt-6-astra",
		Instructions: "You are a helpful math tutor. Guide the user through the solution step by step.",
	})

	response := p.Gen(context.Background(), []*llm.Message{
		{
			Role: llm.RoleUser,
			Contents: llm.MessageContents{
				llm.NewTextContent("how can I solve 8x + 7 = -23"),
			},
		},
	}, llm.ApplyGenOptions([]llm.WithGenOption{llm.WithStream(true), llm.WithOutputFormat[MathReasoning]()}))

	for chunk, err := range response {
		if err != nil {
			t.Log(err)
		} else {
			t.Logf("%s", chunk)
		}
	}
}
