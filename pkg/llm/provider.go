package llm

import (
	"context"
	"iter"
)

type ProviderOptions struct {
	ApiKey   string
	BaseURL  string
	Endpoint string
}

type GenOptions struct {
	Instructions string
	Stream       bool
}

type WithGenOption func(*GenOptions)

type Provider interface {
	Gen(
		ctx context.Context,
		model string,
		messages []*Message,
		options ...WithGenOption,
	) iter.Seq2[*ResponseChunk, error]
}

func WithInstructions(instructions string) WithGenOption {
	return func(o *GenOptions) {
		o.Instructions = instructions
	}
}

func WithStream(stream bool) WithGenOption {
	return func(o *GenOptions) {
		o.Stream = stream
	}
}

func ApplyGenOptions(options []WithGenOption) *GenOptions {
	opts := &GenOptions{
		Stream: true,
	}

	for _, opt := range options {
		opt(opts)
	}

	return opts
}
