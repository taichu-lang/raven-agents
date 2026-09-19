package openai

import (
	"github.com/openai/openai-go/v3/responses"
	"github.com/taichu-lang/raven-agents/agent/llm"
)

func responsesFinishReason(resp *responses.Response) string {
	switch resp.Status {
	case responses.ResponseStatusCompleted:
		return llm.FinishReasonDone
	case responses.ResponseStatusIncomplete:
		return resp.IncompleteDetails.Reason
	default:
		return ""
	}
}

func streamEventToResponse(event responses.ResponseStreamEventUnion) (*llm.ResponseChunk, error) {
	switch e := event.AsAny().(type) {
	case responses.ResponseTextDeltaEvent:
		return &llm.ResponseChunk{
			Type:     llm.ResponseChunkTypeDelta,
			Role:     llm.RoleAssistant,
			Contents: llm.MessageContents{llm.NewTextContent(e.Delta)},
		}, nil

	case responses.ResponseOutputItemDoneEvent:
		// TODO(Leo): handle annotations.
		chunk := &llm.ResponseChunk{
			Type:         llm.ResponseChunkTypeFinal,
			Role:         llm.RoleAssistant,
			FinishReason: llm.FinishReasonDone,
		}
		if msg, ok := e.Item.AsAny().(responses.ResponseOutputMessage); ok {
			chunk.Contents = responsesToMessageContents(msg.Content, chunk.Contents)
			return chunk, nil
		}

		if call, ok := e.Item.AsAny().(responses.ResponseFunctionToolCall); ok {
			chunk.Contents = llm.MessageContents{llm.NewToolCallContent(call.CallID, call.Name, call.Arguments)}
			return chunk, nil
		}

	case responses.ResponseCompletedEvent:
		chunk := &llm.ResponseChunk{
			Type:         llm.ResponseChunkTypeUsage,
			Role:         llm.RoleAssistant,
			FinishReason: responsesFinishReason(&e.Response),
		}
		if usage := toUsageContent(e.Response.Usage); usage != nil {
			chunk.Contents = llm.MessageContents{usage}
			return chunk, nil
		}
	}

	return nil, nil
}

func responsesToMessageContents(
	outputs []responses.ResponseOutputMessageContentUnion,
	contents llm.MessageContents,
) llm.MessageContents {
	for _, c := range outputs {
		switch c := c.AsAny().(type) {
		case responses.ResponseOutputText:
			text := llm.NewTextContent(c.Text)
			contents = append(contents, text)
		}
	}

	return contents
}

func toUsageContent(usage responses.ResponseUsage) *llm.UsageContent {
	if usage.InputTokens == 0 && usage.OutputTokens == 0 {
		return nil
	}

	return &llm.UsageContent{
		InputTokenCount:       usage.InputTokens,
		OutputTokenCount:      usage.OutputTokens,
		TotalTokenCount:       usage.TotalTokens,
		CachedInputTokenCount: usage.InputTokensDetails.CachedTokens,
		ReasoningTokenCount:   usage.OutputTokensDetails.ReasoningTokens,
	}
}
