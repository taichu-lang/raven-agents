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

	// ChatURL is used for text generation, ex: https://api.openai.com/v1/responses
	ChatURL string `yaml:"chat_url,omitzero"       json:"chat_url,omitzero"`
	// ImageGenURL is used for image generation, ex: https://api.openai.com/v1/images/generations
	ImageGenURL string `yaml:"image_gen_url,omitzero"  json:"image_gen_url,omitzero"`
	// ImageEditURL is used for image editing, ex: https://api.openai.com/v1/images/edits
	ImageEditURL string `yaml:"image_edit_url,omitzero" json:"image_edit_url,omitzero"`
	// Alias is the model id used in the backend provider.
	Alias string `yaml:"alias,omitempty"         json:"alias,omitzero"`
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

func (o *GenOptions) WithDefaults() *GenOptions {
	options := &GenOptions{
		Stream: true,
	}

	if o.OutputFormat != nil {
		options.OutputFormat = o.OutputFormat
	}

	if o.Tools != nil {
		options.Tools = o.Tools
	}

	return options
}

// ImageStream is the pull-based stream of image generation chunks, mirroring ResponseStream. The terminal chunk
// always carries Result.
type ImageStream = iter.Seq2[*ImageChunk, error]

// Provider defines the full set of capabilities a model provider offers:
// - GenText: input is text/image/file, output is text
// - GenImageStream: input is text/image, output is image. Streaming
// - GenImage: input is text/image, output is image. Non-streaming
//
// If a provider does not support a capability, it may implement it as e.g. panic("not implemented").
// Callers should decide based on the model's catalog.
type Provider interface {
	GenText(ctx context.Context, messages []*Message, options *GenOptions) ResponseStream
	GenImageStream(ctx context.Context, req *ImageRequest) (ImageStream, error)
	GenImage(ctx context.Context, req *ImageRequest) (*ImageResult, error)
}
