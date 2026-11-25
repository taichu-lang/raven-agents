package claude

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/agent/util"
)

// defaultMaxTokens is used when the catalog does not declare an output limit; the messages api requires
// max_tokens.
const defaultMaxTokens = 8192

// messagesPathSuffix is the endpoint the SDK appends to the base url on its own.
const messagesPathSuffix = "/v1/messages"

// Provider implements underlying.Provider with the Claude messages api. Only text generation is supported.
type Provider struct {
	client  anthropic.Client
	options *underlying.ProviderOptions
	logger  *slog.Logger
}

var _ underlying.Provider = (*Provider)(nil)

func NewProvider(options *underlying.ProviderOptions) *Provider {
	opts := []option.RequestOption{
		option.WithAPIKey(options.ApiKey),
		option.WithHTTPClient(util.StreamClient),
	}

	// ChatURL is the full endpoint (ex: https://api.anthropic.com/v1/messages), while the SDK expects the base
	// url and appends the endpoint path itself.
	if options.ChatURL != "" {
		opts = append(opts, option.WithBaseURL(strings.TrimSuffix(options.ChatURL, messagesPathSuffix)))
	}

	return &Provider{
		client:  anthropic.NewClient(opts...),
		options: options,
		logger:  slog.With("provider", "claude"),
	}
}

func (p *Provider) modelID() string {
	if p.options.Alias != "" {
		return p.options.Alias
	}

	return p.options.Catalog.ID
}

func (p *Provider) maxTokens() int64 {
	if limit := p.options.Catalog.Limit.Output; limit > 0 {
		return int64(limit)
	}

	return defaultMaxTokens
}

func (p *Provider) GenText(
	ctx context.Context,
	messages []*underlying.Message,
	options *underlying.GenOptions,
) underlying.ResponseStream {
	return func(yield func(*underlying.ResponseChunk, error) bool) {
		opts := options.WithDefaults()
		params, err := p.newMessageParams(messages, opts)
		if err != nil {
			p.logger.Error("invalid generation request", "err", err)
			yield(nil, err)
			return
		}

		p.logger.Debug("request of generation", "model", params.Model, "options", opts)

		if opts.Stream {
			p.genStreaming(ctx, params, yield)
			return
		}

		response, err := p.client.Messages.New(ctx, *params)
		if err != nil {
			p.logger.Error("failed to get generation response in non-stream mode", "err", err)
			yield(nil, err)
			return
		}

		yieldFinal(response, yield)
	}
}

func (p *Provider) genStreaming(
	ctx context.Context,
	params *anthropic.MessageNewParams,
	yield func(*underlying.ResponseChunk, error) bool,
) {
	streaming := p.client.Messages.NewStreaming(ctx, *params)
	defer streaming.Close()

	accumulated := anthropic.Message{}
	for streaming.Next() {
		event := streaming.Current()
		if err := accumulated.Accumulate(event); err != nil {
			p.logger.Error("failed to accumulate stream event", "err", err)
			yield(nil, err)
			return
		}

		if chunk := deltaChunk(accumulated.ID, event); chunk != nil && !yield(chunk, nil) {
			return
		}
	}

	if err := streaming.Err(); err != nil {
		p.logger.Error("generation stream failed", "err", err)
		yield(nil, err)
		return
	}

	yieldFinal(&accumulated, yield)
}

// yieldFinal emits the `usage` chunk followed by the `final` chunk, keeping the order of the other providers.
func yieldFinal(message *anthropic.Message, yield func(*underlying.ResponseChunk, error) bool) {
	if usage := usageChunk(&message.Usage); usage != nil && !yield(usage, nil) {
		return
	}

	yield(finalChunk(message), nil)
}

func (p *Provider) GenImageStream(
	_ context.Context,
	_ *underlying.ImageRequest,
) (underlying.ImageStream, error) {
	return nil, fmt.Errorf("claude: image generation: %w", errors.ErrUnsupported)
}

func (p *Provider) GenImage(
	_ context.Context,
	_ *underlying.ImageRequest,
) (*underlying.ImageResult, error) {
	return nil, fmt.Errorf("claude: image generation: %w", errors.ErrUnsupported)
}
