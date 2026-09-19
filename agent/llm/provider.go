package llm

import (
	"context"
	"iter"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/taichu-lang/raven-agents/tool"
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

type OutputFormatOption struct {
	Schema   *jsonschema.Schema
	TypeName string
}

type GenOptions struct {
	Stream bool

	// OutputFormat is the schema of response what we want to get from llm, i.e.,
	// structured model outputs.
	OutputFormat *OutputFormatOption

	Tools []tool.Tool
}

type WithGenOption func(*GenOptions)

type Provider interface {
	Gen(
		ctx context.Context,
		messages []*Message,
		options *GenOptions,
	) ResponseStream
}

func WithStream(stream bool) WithGenOption {
	return func(o *GenOptions) {
		o.Stream = stream
	}
}

func WithOutputFormat[Out any]() WithGenOption {
	schema, err := tool.SchemaFor[Out]()
	if err != nil {
		panic(err)
	}

	var zero Out
	t := reflect.TypeOf(&zero).Elem()

	return func(o *GenOptions) {
		o.OutputFormat = &OutputFormatOption{
			Schema:   schema,
			TypeName: t.Name(),
		}
	}
}

func WithTool(t tool.Tool) WithGenOption {
	return func(o *GenOptions) {
		o.Tools = append(o.Tools, t)
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

func WithDefaultOptions(options *GenOptions) *GenOptions {
	if options == nil {
		return &GenOptions{
			Stream: true,
		}
	}

	return options
}
