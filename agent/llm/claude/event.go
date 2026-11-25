package claude

import (
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
)

// deltaChunk converts a stream event into a delta chunk, or nil when the event carries no displayable delta.
func deltaChunk(messageID string, event anthropic.MessageStreamEventUnion) *underlying.ResponseChunk {
	if event.Type != "content_block_delta" {
		return nil
	}

	switch event.Delta.Type {
	case "text_delta":
		return &underlying.ResponseChunk{
			ID:       messageID,
			Role:     underlying.RoleAssistant,
			Contents: underlying.MessageContents{underlying.NewTextContent(event.Delta.Text)},
			Type:     underlying.ResponseChunkTypeDelta,
		}

	case "thinking_delta":
		return &underlying.ResponseChunk{
			ID:       messageID,
			Role:     underlying.RoleAssistant,
			Contents: underlying.MessageContents{underlying.NewTextContent(event.Delta.Thinking)},
			Type:     underlying.ResponseChunkTypeReasoningDelta,
		}
	}

	return nil
}

func finalChunk(message *anthropic.Message) *underlying.ResponseChunk {
	contents := make(underlying.MessageContents, 0, len(message.Content))

	for _, block := range message.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				contents = append(contents, underlying.NewTextContent(block.Text))
			}

		case "thinking":
			reasoning := underlying.NewReasoningContent("", block.Thinking)
			reasoning.EncryptedContent = block.Signature
			contents = append(contents, reasoning)

		case "redacted_thinking":
			reasoning := underlying.NewReasoningContent(redactedThinkingID, "")
			reasoning.EncryptedContent = block.Data
			contents = append(contents, reasoning)

		case "tool_use":
			arguments := string(block.Input)
			if arguments == "" {
				arguments = "{}"
			}

			contents = append(contents, underlying.NewToolCallContent(block.ID, block.Name, "", arguments))
		}
	}

	return &underlying.ResponseChunk{
		ID:           message.ID,
		Role:         underlying.RoleAssistant,
		Contents:     contents,
		Type:         underlying.ResponseChunkTypeFinal,
		FinishReason: finishReason(message.StopReason),
	}
}

func finishReason(reason anthropic.StopReason) string {
	switch reason {
	case anthropic.StopReasonEndTurn, anthropic.StopReasonToolUse, anthropic.StopReasonStopSequence:
		return underlying.FinishReasonDone
	default:
		return string(reason)
	}
}

func usageChunk(usage *anthropic.Usage) *underlying.ResponseChunk {
	if usage.InputTokens == 0 && usage.OutputTokens == 0 {
		return nil
	}

	// Unlike other providers, input_tokens of the messages api excludes cached tokens; add them back so Input
	// follows the convention of UsageContent.
	input := usage.InputTokens + usage.CacheReadInputTokens + usage.CacheCreationInputTokens

	return &underlying.ResponseChunk{
		Role: underlying.RoleAssistant,
		Contents: underlying.MessageContents{&underlying.UsageContent{
			Input:      input,
			Output:     usage.OutputTokens,
			Total:      input + usage.OutputTokens,
			CacheRead:  usage.CacheReadInputTokens,
			CacheWrite: usage.CacheCreationInputTokens,
			Reasoning:  usage.OutputTokensDetails.ThinkingTokens,
		}},
		Type: underlying.ResponseChunkTypeUsage,
	}
}
