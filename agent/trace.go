package agent

import (
	"context"
	"fmt"

	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/internal/event"
	"github.com/taichu-lang/raven-agents/internal/observability"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type TraceSubscriber struct {
	tracer trace.Tracer

	turnCtx  context.Context
	turnSpan trace.Span

	// ctx and span belonging to current iteration.
	iterationCtx  context.Context
	iterationSpan trace.Span
}

func NewTraceSubscriber(root string) *TraceSubscriber {
	tp := otel.GetTracerProvider()
	if tp == nil {
		panic("Invoke NewTracerProvider first")
	}

	return &TraceSubscriber{
		tracer: tp.Tracer(root),
	}
}

func (s *TraceSubscriber) OnEvent(e *event.Event) {
	switch e.Name {
	case event.EventTurnStart:
		s.onTurnStart(e.Payload.(*event.TurnStartPayload))

	case event.EventTurnEnd:
		s.turnSpan.End()
		s.turnSpan = nil
		s.turnCtx = nil

	case event.EventIterationStart:
		s.onIterationStart(e.Payload.(*event.IterationStartPayload))

	case event.EventIterationEnd:
		s.iterationSpan.End()
		s.iterationSpan = nil
		s.iterationCtx = nil

	case event.EventLLMComplete:
		message := e.Payload.(*underlying.Message)
		s.onLLMComplete(message)
	}
}

func (s *TraceSubscriber) onTurnStart(turn *event.TurnStartPayload) {
	s.turnCtx, s.turnSpan = s.tracer.Start(
		context.Background(),
		"turn.start",
	)
	s.turnSpan.SetAttributes(
		attribute.String(observability.OpenInferenceSpanKind, observability.SpanKindAgent),
		attribute.String(observability.LLMProvider, turn.Provider),
		attribute.String(observability.LLMModelName, turn.Model),
	)

	for i, m := range turn.Input {
		s.turnSpan.SetAttributes(
			attribute.String(observability.LLMInputMessageRoleKey(i), string(m.Role)),
			attribute.String(observability.LLMInputMessageContentKey(i), m.Contents.CollectText()),
		)
	}
}

func (s *TraceSubscriber) onIterationStart(payload *event.IterationStartPayload) {
	if s.iterationSpan != nil {
		s.iterationSpan.End()
	}

	s.iterationCtx, s.iterationSpan = s.tracer.Start(
		s.turnCtx,
		fmt.Sprintf("iteration.%d", payload.Iteration),
	)
	s.iterationSpan.SetAttributes(
		attribute.String(observability.OpenInferenceSpanKind, observability.SpanKindLLM),
	)

	for i, m := range payload.Input {
		s.iterationSpan.SetAttributes(
			attribute.String(observability.LLMInputMessageRoleKey(i), string(m.Role)),
			attribute.String(observability.LLMInputMessageContentKey(i), m.Contents.CollectText()),
		)
	}
}

func (s *TraceSubscriber) onLLMComplete(message *underlying.Message) {
	s.iterationSpan.SetAttributes(
		attribute.String(observability.LLMOutputMessageRoleKey(0), string(underlying.RoleAssistant)),
		attribute.String(observability.LLMOutputMessageContentKey(0), message.Contents.CollectText()),
	)
}
