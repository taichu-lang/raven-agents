package openai

import (
	"github.com/openai/openai-go/v3/responses"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
)

func responsesFinishReason(resp *responses.Response) string {
	switch resp.Status {
	case responses.ResponseStatusCompleted:
		return underlying.FinishReasonDone
	case responses.ResponseStatusIncomplete:
		return resp.IncompleteDetails.Reason
	default:
		return ""
	}
}

func streamEventToResponse(event responses.ResponseStreamEventUnion) (*underlying.ResponseChunk, error) {
	switch e := event.AsAny().(type) {
	case responses.ResponseTextDeltaEvent:
		return &underlying.ResponseChunk{
			Type:     underlying.ResponseChunkTypeDelta,
			Role:     underlying.RoleAssistant,
			Contents: underlying.MessageContents{underlying.NewTextContent(e.Delta)},
		}, nil

	case responses.ResponseOutputItemDoneEvent:
		// TODO(Leo): handle annotations.
		chunk := &underlying.ResponseChunk{
			Type:         underlying.ResponseChunkTypeFinal,
			Role:         underlying.RoleAssistant,
			FinishReason: underlying.FinishReasonDone,
		}
		if msg, ok := e.Item.AsAny().(responses.ResponseOutputMessage); ok {
			chunk.Contents = responsesToMessageContents(msg.Content, chunk.Contents)
			return chunk, nil
		}

		if call, ok := e.Item.AsAny().(responses.ResponseFunctionToolCall); ok {
			chunk.Contents = underlying.MessageContents{underlying.NewToolCallContent(call.CallID, call.Name, call.Arguments)}
			return chunk, nil
		}

	case responses.ResponseCompletedEvent:
		chunk := &underlying.ResponseChunk{
			Type:         underlying.ResponseChunkTypeUsage,
			Role:         underlying.RoleAssistant,
			FinishReason: responsesFinishReason(&e.Response),
		}
		if usage := toUsageContent(e.Response.Usage); usage != nil {
			chunk.Contents = underlying.MessageContents{usage}
			return chunk, nil
		}
	}

	return nil, nil
}

func responsesToMessageContents(
	outputs []responses.ResponseOutputMessageContentUnion,
	contents underlying.MessageContents,
) underlying.MessageContents {
	for _, c := range outputs {
		switch c := c.AsAny().(type) {
		case responses.ResponseOutputText:
			text := underlying.NewTextContent(c.Text)
			contents = append(contents, text)
		}
	}

	return contents
}

func toUsageContent(usage responses.ResponseUsage) *underlying.UsageContent {
	if usage.InputTokens == 0 && usage.OutputTokens == 0 {
		return nil
	}

	return &underlying.UsageContent{
		InputTokenCount:       usage.InputTokens,
		OutputTokenCount:      usage.OutputTokens,
		TotalTokenCount:       usage.TotalTokens,
		CachedInputTokenCount: usage.InputTokensDetails.CachedTokens,
		ReasoningTokenCount:   usage.OutputTokensDetails.ReasoningTokens,
	}
}
