package underlying

import (
	"context"
	"errors"
	"iter"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/taichu-lang/raven-agents/tool"
)

type ResponseStream = iter.Seq2[*ResponseChunk, error]

// Protocol declares the model family and the api schema used by the provider.
type Protocol string

const (
	// ProtocolCompletion represents OpenAI completion api, endpoint is '/v1/chat/completions'.
	ProtocolCompletion Protocol = "completion"

	// ProtocolResponses represents OpenAI responses api, endpoint is '/v1/responses'.
	ProtocolResponses Protocol = "responses"

	// ProtocolClaude represents Claude message api, endpoint is '/v1/messages'.
	ProtocolClaude Protocol = "claude"
)

// ToolSearchToolName is the broadcast name of the tool search entry (name as protocol): an adapter that
// supports the native form (ex: OpenAI tool_search with client execution) renders the tool carrying this name
// as the native item instead of a plain function tool.
const ToolSearchToolName = "tool_search"

// ProviderOptions is the constructor options of an instance of llm provider. Different models in the same
// family may have different instructions, context windows size, and conversation history (ex: reasoning) may
// not be directly transferable to another model. So by design, each provider instance owns the `model` and
// `instructions`.
type ProviderOptions struct {
	ApiKey       string   `yaml:"api_key"               json:"api_key"`
	Instructions string   `yaml:"instructions,omitzero" json:"instructions,omitzero"`
	Catalog      *Catalog `yaml:"catalog"               json:"catalog"`

	// TextURL is used for text generation, ex: https://api.openai.com/v1/responses
	TextURL string `yaml:"text_url,omitzero"       json:"text_url,omitzero"`
	// ImageGenURL is used for image generation, ex: https://api.openai.com/v1/images/generations
	ImageGenURL string `yaml:"image_gen_url,omitzero"  json:"image_gen_url,omitzero"`
	// ImageEditURL is used for image editing, ex: https://api.openai.com/v1/images/edits
	ImageEditURL string `yaml:"image_edit_url,omitzero" json:"image_edit_url,omitzero"`
}

func (p *ProviderOptions) Validate() error {
	if p.Catalog == nil {
		return errors.New("catalog is required")
	}

	return nil
}

type OutputFormatOption struct {
	Schema   *jsonschema.Schema
	TypeName string
}

type GenOptions struct {
	Stream bool

	// OutputFormat is the schema of response what we want to get from llm, i.e., structured model outputs.
	OutputFormat *OutputFormatOption

	Tools []tool.Tool
}

type WithGenOption func(*GenOptions)

type Provider interface {
	Gen(ctx context.Context, messages []*Message, options *GenOptions) ResponseStream
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

// ImageStream is the pull-based stream of image generation chunks, mirroring ResponseStream. The terminal chunk
// always carries Result.
type ImageStream = iter.Seq2[*ImageChunk, error]

// ImageGenerator is the standalone image generation contract, separate from Provider because image generation
// is a synchronous call outside the chat stream. Editing is the same call with ImageRequest.Images non-empty.
type ImageGenerator interface {
	GenerateImageStream(ctx context.Context, req *ImageRequest) (ImageStream, error)
}

// GenerateImage is the non-streaming convenience wrapper around ImageGenerator: it consumes the stream and
// returns the terminal result. Use GenerateImageStream directly when progressive previews matter (ex:
// user-facing rendering).
func GenerateImage(ctx context.Context, g ImageGenerator, req *ImageRequest) (*ImageResult, error) {
	stream, err := g.GenerateImageStream(ctx, req)
	if err != nil {
		return nil, err
	}

	var result *ImageResult
	for chunk, err := range stream {
		if err != nil {
			return nil, err
		}

		if chunk.Result != nil {
			result = chunk.Result
		}
	}

	if result == nil {
		return nil, errors.New("image generation stream ended without a terminal result")
	}

	return result, nil
}
