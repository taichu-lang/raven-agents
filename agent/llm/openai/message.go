package openai

import "github.com/taichu-lang/raven-agents/agent/llm"

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

type ResponseInputText struct {
	Text string `json:"text"`
}

type ResponseInputImage struct {
	ImageURL string `json:"image_url"`
}

type ResponseInputFile struct {
	FileURL string `json:"file_url"`
}

type ResponseInputContent struct {
	Type InputType `json:"type"`
	*ResponseInputText
	*ResponseInputImage
	*ResponseInputFile
}

type ResponseInputItem struct {
	Role    Role                   `json:"role"`
	Content []ResponseInputContent `json:"content"`
}

type ResponseInput []*ResponseInputItem

type ResponsesParams struct {
	Model           string        `json:"model"`
	Stream          bool          `json:"stream"`
	Instructions    string        `json:"instructions,omitzero"`
	MaxOutputTokens int64         `json:"max_output_tokens,omitzero"`
	Temperature     float64       `json:"temperature,omitzero"`
	Input           ResponseInput `json:"input,omitzero"`
}

func inputFromMessage(message *llm.Message) *ResponseInputItem {
	input := &ResponseInputItem{
		Content: make([]ResponseInputContent, 0, len(message.Contents)),
	}

	switch message.Role {
	case llm.RoleUser:
		input.Role = RoleUser
		for _, mc := range message.Contents {
			input.Content = buildInputContent(InputTypeUserText, mc, input.Content)
		}

	case llm.RoleAssistant:
		input.Role = RoleAssistant
		for _, mc := range message.Contents {
			input.Content = buildInputContent(InputTypeAssistant, mc, input.Content)
		}
	}

	return input
}

func buildInputContent(
	t InputType,
	content llm.MessageContent,
	inputs []ResponseInputContent,
) []ResponseInputContent {
	switch c := content.(type) {
	case *llm.TextContent:
		return append(inputs, ResponseInputContent{
			Type: t,
			ResponseInputText: &ResponseInputText{
				Text: c.Raw(),
			},
		})

	default:
		panic("unsupported message content")
	}
}
