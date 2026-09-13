package llm

import (
	"context"
	"iter"
)

type ResponseStream = iter.Seq2[*ResponseChunk, error]

// Protocol declares the model family and the api schema used by the provider.
type Protocol string

const (
	// ProtocolOpenAI represents OpenAI responses api, endpoint is '/v1/responses'.
	ProtocolOpenAI Protocol = "openai"

	// ProtocolClaude represents Claude message api, endpoint is '/v1/messages'.
	ProtocolClaude Protocol = "claude"
)

type ProviderOptions struct {
	ApiKey   string
	Endpoint string

	// Default is ProtocolOpenAI.
	Protocol Protocol
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
	) ResponseStream
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
