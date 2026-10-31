package openai

import (
	"encoding/json"
	"strings"

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

	// The Responses API streams two kinds of reasoning deltas:
	// - response.reasoning_text.delta: the raw chain-of-thought tokens, only sent when the request includes
	//   "reasoning.text"; verbose and unpolished, it is not meant for end users, so it is deliberately not
	//   consumed here.
	// - response.reasoning_summary_text.delta: a condensed, human-readable summary of the reasoning, enabled
	//   via the reasoning.summary request option; this is what user-facing agents display (ex: ChatGPT's
	//   "thinking" section), so it is the one handled below.

	// Some providers (ex: qwen3.8-max) don't support encrypted_content, so with thinking mode enabled they only
	// output the reasoning summary, which is also delivered via the response.reasoning_text.delta event.
	// TODO(Leo): determine how to detect the form in which a model outputs its reasoning (raw text vs summary),
	// so the two events can be handled accordingly.
	case responses.ResponseReasoningTextDeltaEvent:
		return nil, &underlying.ResponseChunk{
			ID:       e.ItemID,
			Type:     underlying.ResponseChunkTypeReasoningDelta,
			Role:     underlying.RoleAssistant,
			Contents: underlying.MessageContents{underlying.NewTextContent(e.Delta)},
		}

	case responses.ResponseReasoningSummaryTextDeltaEvent:
		return nil, &underlying.ResponseChunk{
			ID:       e.ItemID,
			Type:     underlying.ResponseChunkTypeReasoningDelta,
			Role:     underlying.RoleAssistant,
			Contents: underlying.MessageContents{underlying.NewTextContent(e.Delta)},
		}

	case responses.ResponseOutputItemDoneEvent:
		// One item is completed, no usage.

	// TODO(Leo): if the response is incomplete, there is no 'response.completed' event. The assistant messages
	// should be parsed in the 'response.output_item.done' event.
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
				chunk.Contents = append(chunk.Contents, underlying.NewToolCallContent(call.CallID, call.Name, call.Namespace, call.Arguments))
			}

			if reasoning, ok := output.AsAny().(responses.ResponseReasoningItem); ok {
				chunk.Contents = append(chunk.Contents, reasoningItemToContent(reasoning))
			}

			// There is no dedicated tool search SSE delta event: a client-executed search only surfaces as an
			// output item of the completed response.
			if searchCall, ok := output.AsAny().(responses.ResponseToolSearchCall); ok {
				chunk.Contents = append(chunk.Contents, toolSearchCallToContent(searchCall))
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

// toolSearchCallToContent converts a tool_search_call output item. The SDK decodes the untyped Arguments field
// via gjson, losing the byte-exact JSON, so the raw wire text is captured first and re-marshalling the decoded
// value is only the fallback.
func toolSearchCallToContent(call responses.ResponseToolSearchCall) *underlying.ToolSearchCallContent {
	var args json.RawMessage
	if call.JSON.Arguments.Valid() {
		args = json.RawMessage(call.JSON.Arguments.Raw())
	} else if call.Arguments != nil {
		args, _ = json.Marshal(call.Arguments)
	}

	return underlying.NewToolSearchCallContent(call.ID, call.CallID, args)
}

// reasoningItemToContent converts a completed reasoning item from the response.completed event. The summary
// parts are joined into Summary for display purposes only (ex: replaying the "thinking" section from persisted
// history); it is NOT sent back to the API. Only the item ID and the encrypted content round-trip: the
// encrypted content is the opaque payload the model needs to keep its reasoning context on subsequent turns.
func reasoningItemToContent(item responses.ResponseReasoningItem) *underlying.ReasoningContent {
	var sb strings.Builder
	for _, part := range item.Summary {
		sb.WriteString(part.Text)
	}

	content := underlying.NewReasoningContent(item.ID, sb.String())
	content.EncryptedContent = item.EncryptedContent
	return content
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
