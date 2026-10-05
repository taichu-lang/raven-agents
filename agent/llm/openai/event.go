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

// streamEventToResponse parses the stream event, returns `usage` (if exists), chunk.
func streamEventToResponse(
	event responses.ResponseStreamEventUnion,
) (*underlying.ResponseChunk, *underlying.ResponseChunk) {
	switch e := event.AsAny().(type) {
	case responses.ResponseTextDeltaEvent:
		return nil, &underlying.ResponseChunk{
			ID:       e.ItemID,
			Type:     underlying.ResponseChunkTypeDelta,
			Role:     underlying.RoleAssistant,
			Contents: underlying.MessageContents{underlying.NewTextContent(e.Delta)},
		}

	case responses.ResponseOutputItemDoneEvent:
		// One item is completed, no usage.

	case responses.ResponseCompletedEvent:
		// TODO(Leo): handle annotations.
		chunk := &underlying.ResponseChunk{
			ID:           e.Response.ID,
			Type:         underlying.ResponseChunkTypeFinal,
			Role:         underlying.RoleAssistant,
			FinishReason: responsesFinishReason(&e.Response),
		}
		for _, output := range e.Response.Output {
			if msg, ok := output.AsAny().(responses.ResponseOutputMessage); ok {
				chunk.Contents = responsesToMessageContents(msg.Content, chunk.Contents)
			}

			if call, ok := output.AsAny().(responses.ResponseFunctionToolCall); ok {
				chunk.Contents = append(chunk.Contents, underlying.NewToolCallContent(call.CallID, call.Name, call.Arguments))
			}
		}

		// Do not append usage to the contents of message chunk, as consumers of those chunks are different.
		if uc := toUsageContent(e.Response.Usage); uc != nil {
			usage := &underlying.ResponseChunk{
				Type:     underlying.ResponseChunkTypeUsage,
				Role:     underlying.RoleAssistant,
				Contents: underlying.MessageContents{uc},
			}
			return usage, chunk
		}

		return nil, chunk
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
