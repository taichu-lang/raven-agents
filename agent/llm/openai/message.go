package openai

import (
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleDeveloper Role = "developer"
	RoleAssistant Role = "assistant"
)

type InputType string

const (
	InputTypeUserText  InputType = "input_text"
	InputTypeImage     InputType = "input_image"
	InputTypeFile      InputType = "input_file"
	InputTypeAssistant InputType = "output_text"
)

type InputText struct {
	Text string `json:"text"`
}

type InputImage struct {
	ImageURL string `json:"image_url,omitzero"`
}

type InputFile struct {
	FileURL string `json:"file_url"`
}

// InputMessageContent is the base unit of input message for 'user' and 'developer'.
type InputMessageContent struct {
	Type InputType `json:"type"`
	*InputText
	*InputImage
	*InputFile
}

type InputMessage struct {
	// user or developer
	Role    Role                  `json:"role,omitzero"`
	Content []InputMessageContent `json:"content,omitzero"`
}

type OutputMessage struct {
	Role    Role                  `json:"role,omitzero"` // Always is 'assistant'.
	Content []InputMessageContent `json:"content,omitzero"`
}

// ReasoningItem is a reasoning input item sent back to the API on subsequent turns, so the model keeps its
// reasoning context. EncryptedContent is required in stateless mode or when using zero data retention;
// otherwise the item ID is enough for the API to look the reasoning up. Summary round-trips the display summary
// stored in ReasoningContent as summary_text parts; the API requires the field to be present, so it is sent as
// an empty list when there is no summary.
type ReasoningItem struct {
	Type             string             `json:"type"` // Always is 'reasoning'.
	ID               string             `json:"id"`
	Summary          []ReasoningSummary `json:"summary,omitzero"`
	EncryptedContent string             `json:"encrypted_content,omitzero"`
}

// ReasoningSummary is one summary part of a reasoning input item.
type ReasoningSummary struct {
	Type string `json:"type"` // Always is 'summary_text'.
	Text string `json:"text"`
}

type InputItemUnion struct {
	*InputMessage
	*OutputMessage
	*FunctionCall
	*FunctionCallOutput
	*ReasoningItem
	*ToolSearchCallItem
	*ToolSearchOutputItem
}

// MarshalJSON serializes whichever variant is set. The embedded types share JSON field names (e.g.
// "role"/"content", "type"/"call_id"/"name"), so a plain struct marshal would report those fields as ambiguous
// and drop them; marshaling the set variant directly avoids that conflict.
func (i InputItemUnion) MarshalJSON() ([]byte, error) {
	switch {
	case i.InputMessage != nil:
		return json.Marshal(i.InputMessage)
	case i.OutputMessage != nil:
		return json.Marshal(i.OutputMessage)
	case i.FunctionCall != nil:
		return json.Marshal(i.FunctionCall)
	case i.FunctionCallOutput != nil:
		return json.Marshal(i.FunctionCallOutput)
	case i.ReasoningItem != nil:
		return json.Marshal(i.ReasoningItem)
	case i.ToolSearchCallItem != nil:
		return json.Marshal(i.ToolSearchCallItem)
	case i.ToolSearchOutputItem != nil:
		return json.Marshal(i.ToolSearchOutputItem)
	default:
		return nil, fmt.Errorf("openai: InputItemUnion has no variant set")
	}
}

type InputTextConfig struct {
	Format *InputFormatJSONSchema `json:"format,omitzero"`
}

type InputFormatJSONSchema struct {
	Name   string             `json:"name,omitzero"`
	Type   string             `json:"type"` // Always is 'json_schema'.
	Schema *jsonschema.Schema `json:"schema"`
	Strict bool               `json:"strict,omitzero"`
}

type ResponsesInput []*InputItemUnion

type ResponsesParams struct {
	Model           string           `json:"model"`
	Stream          bool             `json:"stream"`
	Instructions    string           `json:"instructions,omitzero"`
	MaxOutputTokens int64            `json:"max_output_tokens,omitzero"`
	Temperature     float64          `json:"temperature,omitzero"`
	Input           ResponsesInput   `json:"input,omitzero"`
	Text            *InputTextConfig `json:"text,omitzero"`
	Tools           []Tool           `json:"tools,omitzero"`
}

func inputItemsFromMessage(message *underlying.Message) []*InputItemUnion {
	switch message.Role {
	case underlying.RoleUser:
		input := &InputItemUnion{
			InputMessage: &InputMessage{
				Role:    RoleUser,
				Content: make([]InputMessageContent, 0, len(message.Contents)),
			},
		}
		for _, mc := range message.Contents {
			input.InputMessage.Content = inputContentFromUser(mc, input.InputMessage.Content)
		}

		return []*InputItemUnion{input}

	case underlying.RoleAssistant:
		return inputItemsFromAssistant(message.Contents)

	case underlying.RoleTool:
		return inputItemsFromTool(message.Contents)

	default:
		panic("unsupported message role")
	}
}

func inputItemsFromAssistant(contents underlying.MessageContents) []*InputItemUnion {
	items := make([]*InputItemUnion, 0, len(contents))
	chatContent := make([]InputMessageContent, 0, len(contents))

	for _, mc := range contents {
		if reasoning, ok := mc.(*underlying.ReasoningContent); ok {
			items = append(items, &InputItemUnion{
				ReasoningItem: &ReasoningItem{
					Type:             "reasoning",
					ID:               reasoning.ID,
					Summary:          reasoningSummaryFrom(reasoning.Summary),
					EncryptedContent: reasoning.EncryptedContent,
				},
			})
			continue
		}

		if call, ok := mc.(*underlying.ToolCallContent); ok {
			items = append(items, &InputItemUnion{
				FunctionCall: &FunctionCall{
					Type:      CallTypeFunction,
					CallID:    call.ID,
					Name:      call.Name,
					Namespace: call.Namespace,
					Arguments: call.Arguments,
				},
			})
			continue
		}

		// A tool search is a top-level item (sibling of function_call), not message content; it must be handled
		// before the inputContentFromAssistant fall-through.
		if search, ok := mc.(*underlying.ToolSearchCallContent); ok {
			items = append(items, &InputItemUnion{
				ToolSearchCallItem: &ToolSearchCallItem{
					Type:      CallTypeToolSearchCall,
					ID:        search.ID,
					CallID:    search.CallID,
					Execution: toolSearchExecutionClient,
					Status:    toolSearchStatusCompleted,
					Arguments: search.Arguments,
				},
			})
			continue
		}

		chatContent = inputContentFromAssistant(mc, chatContent)
	}

	if len(chatContent) > 0 {
		items = append(items, &InputItemUnion{
			OutputMessage: &OutputMessage{
				Role:    RoleAssistant,
				Content: chatContent,
			},
		})
	}

	return items
}

// reasoningSummaryFrom converts the display summary stored in ReasoningContent back into reasoning summary
// parts for the input item. The API requires the summary field to be present, so an empty summary still yields
// a (non-nil) empty list.
func reasoningSummaryFrom(summary string) []ReasoningSummary {
	// Some provider needs summary to be present even if it is empty.
	parts := make([]ReasoningSummary, 0, 1)
	parts = append(parts, ReasoningSummary{
		Type: "summary_text",
		Text: summary,
	})

	return parts
}

// summaryHeader prefixes a compaction summary when rendered as user input text.
const summaryHeader = "[context summary]\n"

func inputContentFromUser(
	content underlying.MessageContent,
	inputs []InputMessageContent,
) []InputMessageContent {
	switch c := content.(type) {
	case *underlying.TextContent:
		return append(inputs, InputMessageContent{
			Type: InputTypeUserText,
			InputText: &InputText{
				Text: c.Text,
			},
		})

	case *underlying.SummaryContent:
		return append(inputs, InputMessageContent{
			Type: InputTypeUserText,
			InputText: &InputText{
				Text: summaryHeader + c.Text,
			},
		})

	case *underlying.MediaContent:
		imageURI := c.ImageURI()
		if imageURI == "" {
			return inputs
		}

		return append(inputs, InputMessageContent{
			Type: InputTypeImage,
			InputImage: &InputImage{
				ImageURL: imageURI,
			},
		})

	default:
		panic("unsupported message content")
	}
}

func inputContentFromAssistant(
	content underlying.MessageContent,
	inputs []InputMessageContent,
) []InputMessageContent {
	switch c := content.(type) {
	case *underlying.TextContent:
		return append(inputs, InputMessageContent{
			Type: InputTypeAssistant,
			InputText: &InputText{
				Text: c.Text,
			},
		})

	default:
		panic("unsupported message content")
	}
}
