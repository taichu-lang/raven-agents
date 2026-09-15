package openai

import (
	"encoding/base64"
	"fmt"

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

type ResponseInputText struct {
	Text string `json:"text"`
}

type ResponseInputImage struct {
	ImageURL string `json:"image_url,omitzero"`
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
			input.Content = inputContentFromUser(mc, input.Content)
		}

	case llm.RoleAssistant:
		input.Role = RoleAssistant
		for _, mc := range message.Contents {
			input.Content = inputContentFromAssistant(mc, input.Content)
		}
	}

	return input
}

func inputContentFromUser(
	content llm.MessageContent,
	inputs []ResponseInputContent,
) []ResponseInputContent {
	switch c := content.(type) {
	case *llm.TextContent:
		return append(inputs, ResponseInputContent{
			Type: InputTypeUserText,
			ResponseInputText: &ResponseInputText{
				Text: c.Raw(),
			},
		})

	case *llm.DataContent:
		if c.MediaType.Image() {
			encodedData := base64.StdEncoding.EncodeToString(c.Data)
			return append(inputs, ResponseInputContent{
				Type: InputTypeImage,
				ResponseInputImage: &ResponseInputImage{
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
	inputs []ResponseInputContent,
) []ResponseInputContent {
	switch c := content.(type) {
	case *llm.TextContent:
		return append(inputs, ResponseInputContent{
			Type: InputTypeAssistant,
			ResponseInputText: &ResponseInputText{
				Text: c.Raw(),
			},
		})

	default:
		panic("unsupported message content")
	}
}
