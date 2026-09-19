package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/taichu-lang/raven-agents/agent/llm"
	"github.com/taichu-lang/raven-agents/tool"
)

type Middleware interface {
	Run(next RunFunc, ctx context.Context, messages []*llm.Message, options *llm.GenOptions) llm.ResponseStream
}

type middlewareRunner struct {
	Middleware
	next RunFunc
}

func (mr middlewareRunner) Run(
	ctx context.Context,
	messages []*llm.Message,
	options *llm.GenOptions,
) llm.ResponseStream {
	next := func(ctx context.Context, messages []*llm.Message, options *llm.GenOptions) llm.ResponseStream {
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
	messages []*llm.Message,
	options *llm.GenOptions,
) llm.ResponseStream {
	return func(yield func(*llm.ResponseChunk, error) bool) {
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

type HistoryMiddleware struct {
	history HistoryProvider
}

func NewHistoryMiddleware(history HistoryProvider) Middleware {
	return &HistoryMiddleware{
		history: history,
	}
}

func (hm *HistoryMiddleware) Run(
	next RunFunc,
	ctx context.Context,
	messages []*llm.Message,
	options *llm.GenOptions,
) llm.ResponseStream {
	return func(yield func(*llm.ResponseChunk, error) bool) {
		messages, _ = hm.history.Retrieve(ctx, messages)
		for chunk, err := range next(ctx, messages, options) {
			if err == nil && chunk.Type == llm.ResponseChunkTypeFinal {
				// TODO(Leo): handle error.
				_ = hm.history.Store(ctx, &llm.Message{
					Role:     chunk.Role,
					Contents: chunk.Contents,
				})
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
	messages []*llm.Message,
	options *llm.GenOptions,
) llm.ResponseStream {
	return func(yield func(*llm.ResponseChunk, error) bool) {
		for chunk, err := range next(ctx, messages, options) {
			// chunk might be nil if has error.
			if err == nil && chunk.Type == llm.ResponseChunkTypeFinal && options.OutputFormat != nil {
				if formatErr := validateStructuredOutput(options.OutputFormat, chunk.Contents); formatErr != nil {
					err = fmt.Errorf("non structured output: %w", formatErr)
				}
			}

			if !yield(chunk, err) {
				return
			}
		}
	}
}

func validateStructuredOutput(format *llm.OutputFormatOption, contents llm.MessageContents) error {
	var text strings.Builder
	for _, content := range contents {
		if tc, ok := content.(*llm.TextContent); ok {
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

type AutoCallMiddleware struct {
	tools map[string]tool.Tool
}

func NewAutoCallMiddleware(tools []tool.Tool) Middleware {
	m := &AutoCallMiddleware{
		tools: make(map[string]tool.Tool),
	}

	for _, tool := range tools {
		m.tools[tool.Name()] = tool
	}

	return m
}

func (ac *AutoCallMiddleware) Run(
	next RunFunc,
	ctx context.Context,
	messages []*llm.Message,
	options *llm.GenOptions,
) llm.ResponseStream {
	return func(yield func(*llm.ResponseChunk, error) bool) {
		opts := ac.withTools(llm.WithDefaultOptions(options))

		current := messages
		for {
			var calls []*llm.ToolCallContent

			for chunk, err := range next(ctx, current, opts) {
				if err != nil || chunk == nil {
					yield(chunk, err)
					return
				}

				if chunk.Type == llm.ResponseChunkTypeFinal {
					for _, content := range chunk.Contents {
						if call, ok := content.(*llm.ToolCallContent); ok {
							calls = append(calls, call)
						}
					}
				}

				if !yield(chunk, nil) {
					return
				}
			}

			if len(calls) == 0 {
				return
			}

			result := &llm.Message{Role: llm.RoleTool}
			for _, call := range calls {
				result.Contents = append(result.Contents, ac.call(ctx, call))
			}

			current = []*llm.Message{result}
		}
	}
}

// withTools merges the middleware's tools into options.Tools without mutating
// the caller's GenOptions.
func (ac *AutoCallMiddleware) withTools(options *llm.GenOptions) *llm.GenOptions {
	if len(ac.tools) == 0 {
		return options
	}

	opts := *options
	opts.Tools = slices.Clone(options.Tools)
	for _, t := range ac.tools {
		opts.Tools = append(opts.Tools, t)
	}

	return &opts
}

// call executes a tool call with automatic approval and turns its outcome
// (result or error) into a ToolResultContent to feed back to the model.
func (ac *AutoCallMiddleware) call(ctx context.Context, call *llm.ToolCallContent) *llm.ToolResultContent {
	t, ok := ac.tools[call.Name]
	if !ok {
		return llm.NewToolResultContent(call.ID, call.Name, fmt.Sprintf("tool %q not found", call.Name), true)
	}

	ft, ok := t.(tool.FuncTool)
	if !ok {
		return llm.NewToolResultContent(
			call.ID,
			call.Name,
			fmt.Sprintf("tool %q is not callable", call.Name),
			true,
		)
	}

	out, err := ft.Call(ctx, call.Arguments)
	if err != nil {
		return llm.NewToolResultContent(call.ID, call.Name, err.Error(), true)
	}

	encoded, err := json.Marshal(out)
	if err != nil {
		return llm.NewToolResultContent(call.ID, call.Name, err.Error(), true)
	}

	return llm.NewToolResultContent(call.ID, call.Name, string(encoded), false)
}
