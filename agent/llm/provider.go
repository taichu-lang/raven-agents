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

// ProviderOptions is the constructor options of an instance of llm provider.
// Different models in the same family may have different instructions, context
// windows size, and conversation history (ex: reasoning) may not be directly
// transferable to another model. So by design, each provider instance owns
// the `model` and `instructions`.
type ProviderOptions struct {
	ApiKey   string
	Endpoint string

	// Default is ProtocolOpenAI.
	Protocol     Protocol
	Model        string
	Instructions string
}

type GenOptions struct {
	Stream bool
}

type WithGenOption func(*GenOptions)

type Provider interface {
	Gen(
		ctx context.Context,
		messages []*Message,
		options ...WithGenOption,
	) ResponseStream
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
