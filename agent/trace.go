package agent

import (
	"context"
	"fmt"

	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/internal/event"
	"github.com/taichu-lang/raven-agents/internal/observability"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type TraceSubscriber struct {
	userID string
	tracer trace.Tracer

	agentCtx  context.Context
	agentSpan trace.Span

	turnCtx  context.Context
	turnSpan trace.Span

	// ctx and span belonging to current iteration.
	iterationCtx  context.Context
	iterationSpan trace.Span

	// span of the llm call inside the current iteration; open between llm.start and
	// llm.complete (or llm.error).
	llmSpan trace.Span

	// provider and model of the current turn, captured on turn.start and forwarded to each
	// llm span.
	provider string
	model    string

	// toolSpans holds the in-flight tool spans keyed by tool call id. Tools of one iteration
	// execute concurrently, so their spans overlap in time and are closed as the results arrive;
	// the event consumer goroutine is single, so the map needs no locking.
	toolSpans map[string]trace.Span
}

func NewTraceSubscriber(root string, userID string) *TraceSubscriber {
	tp := otel.GetTracerProvider()
	if tp == nil {
		panic("Invoke NewTracerProvider first")
	}

	tracer := tp.Tracer(root)
	agentCtx, agentSpan := tracer.Start(context.Background(), root)
	agentSpan.SetAttributes(
		attribute.String(observability.UserID, userID),
		attribute.String(observability.OpenInferenceSpanKind, observability.SpanKindAgent),
	)

	return &TraceSubscriber{
		userID:    userID,
		tracer:    tracer,
		agentCtx:  agentCtx,
		agentSpan: agentSpan,
		toolSpans: make(map[string]trace.Span),
	}
}

func (s *TraceSubscriber) Close() {
	s.agentSpan.End()
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
		s.onIterationStart(e.Payload.(int64))

	case event.EventIterationEnd:
		s.endPendingToolSpans()
		s.endLLMSpan()
		s.iterationSpan.End()
		s.iterationSpan = nil
		s.iterationCtx = nil

	case event.EventLLMStart:
		s.onLLMStart(e.Payload.([]*underlying.Message))

	case event.EventLLMUsage:
		s.onLLMUsage(e.Payload.(*underlying.UsageContent))

	case event.EventLLMComplete:
		message := e.Payload.(*underlying.Message)
		s.onLLMComplete(message)

	case event.EventLLMError:
		s.onLLMError(e.Payload.(error))

	case event.EventToolCall:
		s.onToolCall(e.Payload.(*underlying.ToolCallContent))

	case event.EventToolResult:
		s.onToolResult(e.Payload.(*underlying.Message))
	}
}

func (s *TraceSubscriber) onTurnStart(turn *event.TurnStartPayload) {
	s.turnCtx, s.turnSpan = s.tracer.Start(
		s.agentCtx,
		fmt.Sprintf("turn.#%d", turn.Turn),
	)
	s.turnSpan.SetAttributes(
		attribute.String(observability.OpenInferenceSpanKind, observability.SpanKindAgent),
		attribute.String(observability.LLMProvider, turn.Provider),
		attribute.String(observability.LLMModelName, turn.Model),
	)
	s.provider = turn.Provider
	s.model = turn.Model

	for i, m := range turn.Messages {
		attrs := []attribute.KeyValue{
			attribute.String(observability.LLMInputMessageRoleKey(i), string(m.Role)),
		}
		attrs = append(
			attrs, messageContentAttributes(m.Contents, observability.LLMInputMessageContentsKey, i)...,
		)
		s.turnSpan.SetAttributes(attrs...)
	}
}

func (s *TraceSubscriber) onIterationStart(iteration int64) {
	if s.iterationSpan != nil {
		s.endPendingToolSpans()
		s.endLLMSpan()
		s.iterationSpan.End()
	}

	s.iterationCtx, s.iterationSpan = s.tracer.Start(
		s.turnCtx,
		fmt.Sprintf("iteration.#%d", iteration),
	)
	s.iterationSpan.SetAttributes(
		attribute.String(observability.OpenInferenceSpanKind, observability.SpanKindChain),
	)
}

// onLLMStart opens the LLM span as a child of the current iteration and records the messages sent
// to the model, which include the tool results of this iteration.
func (s *TraceSubscriber) onLLMStart(input []*underlying.Message) {
	_, s.llmSpan = s.tracer.Start(s.iterationCtx, "chat "+s.model)
	s.llmSpan.SetAttributes(
		attribute.String(observability.OpenInferenceSpanKind, observability.SpanKindLLM),
		attribute.String(observability.LLMProvider, s.provider),
		attribute.String(observability.LLMModelName, s.model),
	)

	for i, m := range input {
		attrs := []attribute.KeyValue{
			attribute.String(observability.LLMInputMessageRoleKey(i), string(m.Role)),
		}
		attrs = append(
			attrs, messageContentAttributes(m.Contents, observability.LLMInputMessageContentsKey, i)...,
		)
		s.llmSpan.SetAttributes(attrs...)
	}
}

// onLLMUsage records the token usage on the open llm span. The agent guarantees that the usage
// event precedes the completion event, so the span is still open here.
func (s *TraceSubscriber) onLLMUsage(usage *underlying.UsageContent) {
	if s.llmSpan == nil {
		return
	}

	s.llmSpan.SetAttributes(
		attribute.Int64(observability.LLMTokenCountPrompt, usage.InputTokenCount),
		attribute.Int64(observability.LLMTokenCountPromptDetailsCacheRead, usage.CachedInputTokenCount),
		attribute.Int64(observability.LLMTokenCountCompletion, usage.OutputTokenCount),
		attribute.Int64(observability.LLMTokenCountCompletionDetailsReasoning, usage.ReasoningTokenCount),
		attribute.Int64(observability.LLMTokenCountTotal, usage.TotalTokenCount),
	)
}

func (s *TraceSubscriber) onLLMComplete(message *underlying.Message) {
	if s.llmSpan == nil {
		return
	}

	attrs := []attribute.KeyValue{
		attribute.String(observability.LLMOutputMessageRoleKey(0), string(underlying.RoleAssistant)),
	}
	attrs = append(
		attrs, messageContentAttributes(message.Contents, observability.LLMOutputMessageContentsKey, 0)...,
	)
	s.llmSpan.SetAttributes(attrs...)
	s.llmSpan.End()
	s.llmSpan = nil
}

func (s *TraceSubscriber) onLLMError(err error) {
	if s.llmSpan == nil {
		return
	}

	s.llmSpan.SetStatus(codes.Error, err.Error())
	s.llmSpan.End()
	s.llmSpan = nil
}

// endLLMSpan closes the llm span if it is still open, so that it does not outlive its iteration.
func (s *TraceSubscriber) endLLMSpan() {
	if s.llmSpan == nil {
		return
	}

	s.llmSpan.SetStatus(codes.Error, "llm span ended without completion")
	s.llmSpan.End()
	s.llmSpan = nil
}

// onToolCall opens a TOOL span for one tool invocation as a child of the current iteration. Tools
// of one iteration run concurrently, so their spans are siblings that overlap in time; each is
// kept in toolSpans until its result arrives in onToolResult.
func (s *TraceSubscriber) onToolCall(call *underlying.ToolCallContent) {
	_, span := s.tracer.Start(s.iterationCtx, call.Name)
	span.SetAttributes(
		attribute.String(observability.OpenInferenceSpanKind, observability.SpanKindTool),
		attribute.String(observability.ToolCallID, call.ID),
		attribute.String(observability.ToolName, call.Name),
		attribute.String(observability.ToolParameters, call.Arguments),
	)
	s.toolSpans[call.ID] = span
}

// onToolResult closes the tool spans of one iteration: the payload is the role=tool message whose
// contents carry every result, so each result is matched back to its call by the tool call id.
func (s *TraceSubscriber) onToolResult(message *underlying.Message) {
	for _, content := range message.Contents {
		result, ok := content.(*underlying.ToolResultContent)
		if !ok {
			continue
		}

		span, ok := s.toolSpans[result.ToolCallID]
		if !ok {
			continue
		}
		delete(s.toolSpans, result.ToolCallID)

		span.SetAttributes(attribute.String(observability.OutputValue, result.Result))
		if result.IsError {
			span.SetStatus(codes.Error, result.Result)
		}
		span.End()
	}
}

// endPendingToolSpans closes the tool spans that never received a result, so that they do not
// outlive the iteration they belong to.
func (s *TraceSubscriber) endPendingToolSpans() {
	for id, span := range s.toolSpans {
		span.SetStatus(codes.Error, "tool span ended without result")
		span.End()
		delete(s.toolSpans, id)
	}
}

// messageContentAttributes converts message contents into per-item attributes, recording each
// item's fields under keys specific to its content type (text, skill, tool_call, tool_result).
// The key function builds the attribute key for the j-th content item of the i-th message; the
// content type itself is always recorded under MessageContentType. Kinds without dedicated
// attributes (usage, data, uri) are recorded by type only.
func messageContentAttributes(
	contents underlying.MessageContents,
	key func(i, j int, child string) string,
	i int,
) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, len(contents)*2)
	for j, content := range contents {
		attrs = append(attrs, attribute.String(
			key(i, j, observability.MessageContentType), string(content.Kind()),
		))

		switch c := content.(type) {
		case *underlying.TextContent:
			attrs = append(attrs, attribute.String(
				key(i, j, observability.MessageContentText), c.Text,
			))
		case *underlying.SkillContent:
			attrs = append(attrs, attribute.String(
				key(i, j, observability.MessageContentSkillName), c.Name,
			))
			if c.Description != "" {
				attrs = append(attrs, attribute.String(
					key(i, j, observability.MessageContentSkillDescription), c.Description,
				))
			}
		case *underlying.ToolCallContent:
			attrs = append(attrs,
				attribute.String(key(i, j, observability.ToolCallID), c.ID),
				attribute.String(key(i, j, observability.ToolCallFunctionName), c.Name),
				attribute.String(key(i, j, observability.ToolCallFunctionArgumentsJSON), c.Arguments),
			)
		case *underlying.ToolResultContent:
			attrs = append(attrs,
				attribute.String(key(i, j, observability.ToolCallID), c.ToolCallID),
				attribute.String(key(i, j, observability.MessageContentText), c.Result),
			)
		}
	}

	return attrs
}
