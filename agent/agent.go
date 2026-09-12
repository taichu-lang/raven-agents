package agent

import (
	"context"
	"iter"
	"log/slog"

	"github.com/taichu-lang/raven-agents/agent/llm"
)

type ResponseStream = iter.Seq2[*llm.ResponseChunk, error]

// RunFunc declares the abstract entrypoint for nodes (ex: llm, middleware) in the agent.
type RunFunc = func(ctx context.Context, messages []*llm.Message) ResponseStream

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

func NewAgent(cfg *Config, llmProvider llm.Provider) *Agent {
	a := &Agent{
		cfg:         cfg,
		logger:      slog.Default().With("agent", cfg.Name),
		llmProvider: llmProvider,
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

func (a *Agent) RunText(ctx context.Context, text string) ResponseStream {
	return a.run(ctx, []*llm.Message{
		{
			Role: llm.RoleUser,
			Contents: llm.MessageContents{
				llm.NewTextContent(text),
			},
		},
	})
}

func (a *Agent) invoke(ctx context.Context, messages []*llm.Message) ResponseStream {
	return a.llmProvider.Gen(ctx, a.cfg.Model, messages, a.options...)
}
