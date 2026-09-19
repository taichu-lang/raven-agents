package openai

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/taichu-lang/raven-agents/agent/llm"
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

type InputItemUnion struct {
	*InputMessage
	*OutputMessage
	*FunctionCall
	*FunctionCallOutput
}

// MarshalJSON serializes whichever variant is set. The embedded types share
// JSON field names (e.g. "role"/"content", "type"/"call_id"/"name"), so a
// plain struct marshal would report those fields as ambiguous and drop them;
// marshaling the set variant directly avoids that conflict.
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

func inputItemsFromMessage(message *llm.Message) []*InputItemUnion {
	switch message.Role {
	case llm.RoleUser:
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

	case llm.RoleAssistant:
		return inputItemsFromAssistant(message.Contents)

	case llm.RoleTool:
		return inputItemsFromTool(message.Contents)

	default:
		panic("unsupported message role")
	}
}

func inputItemsFromAssistant(contents llm.MessageContents) []*InputItemUnion {
	items := make([]*InputItemUnion, 0, len(contents))
	chatContent := make([]InputMessageContent, 0, len(contents))

	for _, mc := range contents {
		if call, ok := mc.(*llm.ToolCallContent); ok {
			items = append(items, &InputItemUnion{
				FunctionCall: &FunctionCall{
					Type:      CallTypeFunction,
					CallID:    call.ID,
					Name:      call.Name,
					Arguments: call.Arguments,
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

func inputContentFromUser(
	content llm.MessageContent,
	inputs []InputMessageContent,
) []InputMessageContent {
	switch c := content.(type) {
	case *llm.TextContent:
		return append(inputs, InputMessageContent{
			Type: InputTypeUserText,
			InputText: &InputText{
				Text: c.Raw(),
			},
		})

	case *llm.DataContent:
		if c.MediaType.Image() {
			encodedData := base64.StdEncoding.EncodeToString(c.Data)
			return append(inputs, InputMessageContent{
				Type: InputTypeImage,
				InputImage: &InputImage{
					ImageURL: fmt.Sprintf("data:%s;base64,%s", c.MediaType, encodedData),
				},
			})
		}

		return inputs

	default:
		panic("unsupported message content")
	}
}

func inputContentFromAssistant(
	content llm.MessageContent,
	inputs []InputMessageContent,
) []InputMessageContent {
	switch c := content.(type) {
	case *llm.TextContent:
		return append(inputs, InputMessageContent{
			Type: InputTypeAssistant,
			InputText: &InputText{
				Text: c.Raw(),
			},
		})

	default:
		panic("unsupported message content")
	}
}
