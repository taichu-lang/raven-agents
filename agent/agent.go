package agent

import (
	"context"
	"log/slog"
	"uuid"

	"github.com/taichu-lang/raven-agents/agent/llm"
	"github.com/taichu-lang/raven-agents/tool"
)

type CtxKeyMessageID struct{}

// RunFunc declares the abstract entrypoint for nodes (ex: llm, middleware) in the agent.
type RunFunc = func(ctx context.Context, messages []*llm.Message, options *llm.GenOptions) llm.ResponseStream

type Config struct {
	Name        string
	Middlewares []Middleware
	Tools       []tool.Tool
}

type Agent struct {
	cfg         *Config
	logger      *slog.Logger
	llmProvider llm.Provider

	// run is the entrypoint for the agent, which combines all the middlewares.
	run     RunFunc
	history HistoryProvider
}

func NewAgent(cfg *Config, providerOptions *llm.ProviderOptions) *Agent {
	a := &Agent{
		cfg:         cfg,
		logger:      slog.Default().With("agent", cfg.Name),
		llmProvider: NewProvider(providerOptions),
		history:     NewHistoryProvider(),
	}

	middlewares := cfg.Middlewares
	if len(cfg.Tools) > 0 {
		middlewares = append(middlewares, NewAutoCallMiddleware(cfg.Tools))
	}

	middlewares = append(
		middlewares,
		NewHistoryMiddleware(a.history),
		NewStructuredOutputMiddleware(),
		NewLoggerMiddleware(a.logger),
		NewTraceMiddleware(cfg.Name),
	)
	// history::run
	//   -> structured_output::run
	//     -> logger::run
	//       -> trace::run
	//          -> llm::gen
	//          <- llm::chunk
	//       <- trace::next
	//     <- logger::next
	//   <- structured_output::next
	// <- history::next
	chain := compileRunChain(a.invoke, middlewares)
	a.run = func(ctx context.Context, messages []*llm.Message, options *llm.GenOptions) llm.ResponseStream {
		ctx, messages, options = a.beforeRun(ctx, messages, options)
		return chain(ctx, messages, options)
	}

	return a
}

func (a *Agent) RunText(ctx context.Context, text string) llm.ResponseStream {
	return a.run(ctx, []*llm.Message{
		{
			ID:   uuid.New().String(),
			Role: llm.RoleUser,
			Contents: llm.MessageContents{
				llm.NewTextContent(text),
			},
		},
	}, nil)
}

func (a *Agent) Run(
	ctx context.Context,
	messages []*llm.Message,
	options ...llm.WithGenOption,
) llm.ResponseStream {
	opts := llm.ApplyGenOptions(options)
	return a.run(ctx, messages, opts)
}

func (a *Agent) invoke(
	ctx context.Context,
	messages []*llm.Message,
	options *llm.GenOptions,
) llm.ResponseStream {
	return a.llmProvider.Gen(ctx, messages, options)
}

func (a *Agent) beforeRun(
	ctx context.Context,
	messages []*llm.Message,
	options *llm.GenOptions,
) (context.Context, []*llm.Message, *llm.GenOptions) {
	ctx = context.WithValue(ctx, CtxKeyMessageID{}, uuid.New().String())
	options = llm.WithDefaultOptions(options)
	return ctx, messages, options
}
