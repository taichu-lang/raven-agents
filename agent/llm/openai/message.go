package openai

type Role string

const (
	RoleUser      Role = "user"
	RoleDeveloper Role = "developer"
	RoleAssistant Role = "assistant"
)

type InputType string

const (
	InputTypeText  InputType = "input_text"
	InputTypeImage InputType = "input_image"
	InputTypeFile  InputType = "input_file"
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

type ResponseInput []ResponseInputItem

type ResponsesParams struct {
	Model           string        `json:"model"`
	Stream          bool          `json:"stream"`
	Instructions    string        `json:"instructions,omitzero"`
	MaxOutputTokens int64         `json:"max_output_tokens,omitzero"`
	Temperature     float64       `json:"temperature,omitzero"`
	Input           ResponseInput `json:"input,omitzero"`
}
