package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
)

type Middleware interface {
	Run(
		next RunFunc,
		ctx context.Context,
		messages []*underlying.Message,
		options *underlying.GenOptions,
	) underlying.ResponseStream
}

type middlewareRunner struct {
	Middleware
	next RunFunc
}

func (mr middlewareRunner) Run(
	ctx context.Context,
	messages []*underlying.Message,
	options *underlying.GenOptions,
) underlying.ResponseStream {
	next := func(ctx context.Context, messages []*underlying.Message, options *underlying.GenOptions) underlying.ResponseStream {
		return mr.next(ctx, messages, options)
	}

	return mr.Middleware.Run(next, ctx, messages, options)
}

// compileRunChain builds a chain of middlewares, where each middleware is applied in reverse order.
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

func (lm *LoggerMiddleware) Run(
	next RunFunc,
	ctx context.Context,
	messages []*underlying.Message,
	options *underlying.GenOptions,
) underlying.ResponseStream {
	return func(yield func(*underlying.ResponseChunk, error) bool) {
		for chunk, err := range next(ctx, messages, options) {
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

type StructuredOutputMiddleware struct {
}

func NewStructuredOutputMiddleware() Middleware {
	return &StructuredOutputMiddleware{}
}

func (s *StructuredOutputMiddleware) Run(
	next RunFunc,
	ctx context.Context,
	messages []*underlying.Message,
	options *underlying.GenOptions,
) underlying.ResponseStream {
	return func(yield func(*underlying.ResponseChunk, error) bool) {
		for chunk, err := range next(ctx, messages, options) {
			// chunk might be nil if has error.
			if options != nil && options.OutputFormat != nil {
				if err == nil && chunk.Type == underlying.ResponseChunkTypeFinal {
					if formatErr := validateStructuredOutput(options.OutputFormat, chunk.Contents); formatErr != nil {
						err = fmt.Errorf("non structured output: %w", formatErr)
					}
				}
			}

			if !yield(chunk, err) {
				return
			}
		}
	}
}

func validateStructuredOutput(
	format *underlying.OutputFormatOption,
	contents underlying.MessageContents,
) error {
	var text strings.Builder
	for _, content := range contents {
		if tc, ok := content.(*underlying.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}

	var instance any
	if err := json.Unmarshal([]byte(text.String()), &instance); err != nil {
		return fmt.Errorf("output is not valid json: %w", err)
	}

	resolved, err := format.Schema.Resolve(nil)
	if err != nil {
		return fmt.Errorf("resolve output schema: %w", err)
	}

	return resolved.Validate(instance)
}
