package llm

import (
	"context"
	"log/slog"
	"uuid"

	"github.com/taichu-lang/raven-agents/agent/llm/openai"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
)

// RunFunc declares the abstract entrypoint for nodes (ex: llm, middleware) in the agent.
type RunFunc = func(ctx context.Context, messages []*underlying.Message, options *underlying.GenOptions) underlying.ResponseStream

type Runner struct {
	logger     *slog.Logger
	underlying underlying.Provider

	// entrypoint for the agent, which combines all the middlewares.
	entrypoint RunFunc
}

func NewRunner(options *underlying.ProviderOptions) *Runner {
	a := &Runner{
		logger:     slog.Default().With("runner", "llm"),
		underlying: newProvider(options),
	}

	middlewares := []Middleware{NewStructuredOutputMiddleware(), NewLoggerMiddleware(a.logger)}

	// -> structured_output::run
	//   -> logger::run
	//      -> llm::gen
	//      <- llm::chunk
	//   <- logger::next
	// <- structured_output::next
	chain := compileRunChain(a.invoke, middlewares)
	a.entrypoint = func(ctx context.Context, messages []*underlying.Message, options *underlying.GenOptions) underlying.ResponseStream {
		ctx, messages, options = a.beforeRun(ctx, messages, options)
		return chain(ctx, messages, options)
	}

	return a
}

func (a *Runner) RunText(ctx context.Context, text string) underlying.ResponseStream {
	return a.entrypoint(ctx, []*underlying.Message{
		{
			ID:   uuid.New().String(),
			Role: underlying.RoleUser,
			Contents: underlying.MessageContents{
				underlying.NewTextContent(text),
			},
		},
	}, nil)
}

func (a *Runner) Run(
	ctx context.Context,
	messages []*underlying.Message,
	options ...underlying.WithGenOption,
) underlying.ResponseStream {
	opts := underlying.ApplyGenOptions(options)
	return a.entrypoint(ctx, messages, opts)
}

func (a *Runner) invoke(
	ctx context.Context,
	messages []*underlying.Message,
	options *underlying.GenOptions,
) underlying.ResponseStream {
	return a.underlying.Gen(ctx, messages, options)
}

func (a *Runner) beforeRun(
	ctx context.Context,
	messages []*underlying.Message,
	options *underlying.GenOptions,
) (context.Context, []*underlying.Message, *underlying.GenOptions) {
	options = underlying.WithDefaultOptions(options)
	return ctx, messages, options
}

func newProvider(options *underlying.ProviderOptions) underlying.Provider {
	if options.Protocol == "" {
		options.Protocol = underlying.ProtocolOpenAI
	}

	if options.ApiKey == "" {
		panic("api key is required")
	}

	if options.Endpoint == "" {
		panic("endpoint is required")
	}

	switch options.Protocol {
	case underlying.ProtocolOpenAI:
		return openai.NewProvider(options)

	default:
		return nil
	}
}
