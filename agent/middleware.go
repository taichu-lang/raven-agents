package agent

import (
	"context"
	"log/slog"
	"slices"

	"github.com/taichu-lang/raven-agents/agent/llm"
)

type Middleware interface {
	Run(next RunFunc, ctx context.Context, messages []*llm.Message) llm.ResponseStream
}

type middlewareRunner struct {
	Middleware
	next RunFunc
}

func (mr middlewareRunner) Run(ctx context.Context, messages []*llm.Message) llm.ResponseStream {
	next := func(ctx context.Context, messages []*llm.Message) llm.ResponseStream {
		return mr.next(ctx, messages)
	}

	return mr.Middleware.Run(next, ctx, messages)
}

func compileRunChain(entry RunFunc, middlewares []Middleware) RunFunc {
	for _, m := range slices.Backward(middlewares) {
		if m == nil {
			continue
		}

		entry = middlewareRunner{Middleware: m, next: entry}.Run
	}

	return entry
}

type LoggerMiddleware struct {
	logger *slog.Logger
}

func NewLoggerMiddleware(logger *slog.Logger) Middleware {
	return &LoggerMiddleware{
		logger: logger,
	}
}

func (lm *LoggerMiddleware) Run(next RunFunc, ctx context.Context, messages []*llm.Message) llm.ResponseStream {
	return func(yield func(*llm.ResponseChunk, error) bool) {
		for chunk, err := range next(ctx, messages) {
			if err != nil {
				lm.logger.Error("error during generation", "err", err)
			} else {
				lm.logger.Debug("response chunk of generation", "chunk", chunk)
			}

			if !yield(chunk, err) {
				return
			}
		}
	}
}
