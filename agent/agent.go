package agent

import (
	"context"
	"log/slog"

	"github.com/taichu-lang/raven-agents/agent/llm"
)

// RunFunc declares the abstract entrypoint for nodes (ex: llm, middleware) in the agent.
type RunFunc = func(ctx context.Context, messages []*llm.Message) llm.ResponseStream

type Config struct {
	Name         string
	Model        string
	Instructions string
	Middlewares  []Middleware
}

type Agent struct {
	cfg         *Config
	logger      *slog.Logger
	llmProvider llm.Provider
	options     []llm.WithGenOption

	// run is the entrypoint for the agent, which combines all the middlewares.
	run RunFunc
}

func NewAgent(cfg *Config, providerOptions *llm.ProviderOptions) *Agent {
	a := &Agent{
		cfg:         cfg,
		logger:      slog.Default().With("agent", cfg.Name),
		llmProvider: NewProvider(providerOptions),
	}

	options := []llm.WithGenOption{}
	if cfg.Instructions != "" {
		options = append(options, llm.WithInstructions(cfg.Instructions))
	}

	a.options = options

	middlewares := append(cfg.Middlewares, NewLoggerMiddleware(a.logger))
	a.run = compileRunChain(a.invoke, middlewares)
	return a
}

func (a *Agent) RunText(ctx context.Context, text string) llm.ResponseStream {
	return a.run(ctx, []*llm.Message{
		{
			Role: llm.RoleUser,
			Contents: llm.MessageContents{
				llm.NewTextContent(text),
			},
		},
	})
}

func (a *Agent) invoke(ctx context.Context, messages []*llm.Message) llm.ResponseStream {
	return a.llmProvider.Gen(ctx, a.cfg.Model, messages, a.options...)
}
