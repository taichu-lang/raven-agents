package agent

import (
	"context"

	"github.com/taichu-lang/raven-agents/agent/llm"
	"github.com/taichu-lang/raven-agents/internal/observability"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type TraceMiddleware struct {
	tracer trace.Tracer
}

func NewTraceMiddleware(name string) Middleware {
	tp := otel.GetTracerProvider()
	if tp == nil {
		panic("Invoke NewTracerProvider first")
	}

	return &TraceMiddleware{
		tracer: tp.Tracer(name),
	}
}

func (t *TraceMiddleware) Run(
	next RunFunc,
	ctx context.Context,
	messages []*llm.Message,
	options *llm.GenOptions,
) llm.ResponseStream {
	return func(yield func(*llm.ResponseChunk, error) bool) {
		ctx, span := t.tracer.Start(ctx, "generation.run")
		defer span.End()

		span.SetAttributes(
			attribute.String(observability.OpenInferenceSpanKind, observability.SpanKindLLM),
			attribute.String(observability.LLMSystem, observability.LLMSystemOpenAI),
		)

		for i, m := range messages {
			span.SetAttributes(attribute.String(observability.LLMInputMessageRoleKey(i), string(m.Role)))

			for j, c := range m.Contents {
				switch ct := c.(type) {
				case *llm.TextContent:
					span.SetAttributes(attribute.String(observability.LLMInputMessageContentKey(j), ct.Text))
				}
			}
		}

		for chunk, err := range next(ctx, messages, options) {
			if err != nil || chunk == nil {
				span.RecordError(err)
				yield(chunk, err)
				return
			}

			if chunk.Type == llm.ResponseChunkTypeFinal {
				span.SetAttributes(
					attribute.String(observability.LLMOutputMessageRoleKey(0), string(chunk.Role)),
				)
				for i, content := range chunk.Contents {
					switch ct := content.(type) {
					case *llm.TextContent:
						span.SetAttributes(attribute.String(observability.LLMOutputMessageContentKey(i), ct.Text))
					}
				}
			}

			if !yield(chunk, nil) {
				return
			}
		}
	}
}
