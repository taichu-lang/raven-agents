package openai

import (
	"github.com/openai/openai-go/v3/responses"
	"github.com/taichu-lang/raven-agents/pkg/llm"
)

func streamEventToChunk(event responses.ResponseStreamEventUnion) (*llm.ResponseChunk, error) {
	return nil, nil
}

func outputTextToContent(
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

func responsesUsageToContent(usage responses.ResponseUsage) *llm.UsageContent {
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
