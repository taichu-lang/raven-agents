package llm

import (
	"context"
	"log/slog"

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

// NewRunner builds a Runner with the default middleware chain followed by the given extra middlewares. An extra
// middleware therefore runs closest to the provider and observes the raw response stream, which makes it the
// injection point for per-call concerns like metering.
func NewRunner(options *underlying.ProviderOptions, extra ...Middleware) *Runner {
	a := &Runner{
		logger:     slog.Default().With("runner", "llm"),
		underlying: newProvider(options),
	}

	middlewares := append(
		[]Middleware{NewStructuredOutputMiddleware(), NewLoggerMiddleware(a.logger)},
		extra...,
	)

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
		underlying.NewMessage(underlying.RoleUser, underlying.MessageContents{
			underlying.NewTextContent(text),
		}),
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
	if err := options.Validate(); err != nil {
		panic(err)
	}

	// TODO(Leo): create provider based on protocol.
	return openai.NewProvider(options)
}
