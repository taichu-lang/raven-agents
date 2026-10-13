package underlying

import "encoding/json"

type ContentKind string

const (
	ContentKindText  ContentKind = "text"
	ContentKindURI   ContentKind = "uri"
	ContentKindUsage ContentKind = "usage"

	// ContentKindData is used to store binary and non-binary data, which depends on the media type.
	// Ex: an image file, a text file, etc.
	ContentKindData ContentKind = "data"

	ContentKindToolCall   ContentKind = "tool_call"
	ContentKindToolResult ContentKind = "tool_result"

	// ContentKindSkill discloses a skill to the model. How it reaches the model is left to each
	// provider, as the shape of the request differs between model apis.
	ContentKindSkill ContentKind = "skill"
)

type MediaType string

const (
	MediaTypeImageJPG MediaType = "image/jpeg"
	MediaTypeImagePNG MediaType = "image/png"
)

func (m MediaType) Image() bool {
	return m == MediaTypeImageJPG || m == MediaTypeImagePNG
}

type MessageContent interface {
	json.Marshaler
	Kind() ContentKind
}

type TextContent struct {
	Text string `json:"text"`
}

func NewTextContent(text string) *TextContent {
	return &TextContent{
		Text: text,
	}
}

func (t TextContent) Kind() ContentKind {
	return ContentKindText
}

func (t *TextContent) MarshalJSON() ([]byte, error) {
	type alias TextContent
	tmp := struct {
		*alias
		Type ContentKind `json:"type"`
	}{
		alias: (*alias)(t),
		Type:  t.Kind(),
	}
	return json.Marshal(tmp)
}

type URIContent struct {
	MediaType MediaType
	URI       string
}

func (t URIContent) Kind() ContentKind {
	return ContentKindURI
}

type UsageContent struct {
	InputTokenCount       int64 `json:"input_tokens,omitzero"`
	OutputTokenCount      int64 `json:"output_tokens,omitzero"`
	TotalTokenCount       int64 `json:"total_tokens,omitzero"`
	CachedInputTokenCount int64 `json:"cached_input_tokens,omitzero"`
	ReasoningTokenCount   int64 `json:"reasoning_tokens,omitzero"`
}

func (u UsageContent) Kind() ContentKind {
	return ContentKindUsage
}

func (u *UsageContent) MarshalJSON() ([]byte, error) {
	type alias UsageContent
	tmp := struct {
		*alias
		Type ContentKind `json:"type"`
	}{
		alias: (*alias)(u),
		Type:  u.Kind(),
	}
	return json.Marshal(tmp)
}

type ToolCallContent struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func NewToolCallContent(id, name, arguments string) *ToolCallContent {
	return &ToolCallContent{
		ID:        id,
		Name:      name,
		Arguments: arguments,
	}
}

func (t ToolCallContent) Kind() ContentKind {
	return ContentKindToolCall
}

func (t *ToolCallContent) MarshalJSON() ([]byte, error) {
	type alias ToolCallContent
	tmp := struct {
		*alias
		Type ContentKind `json:"type"`
	}{
		alias: (*alias)(t),
		Type:  t.Kind(),
	}
	return json.Marshal(tmp)
}

type ToolResultContent struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name,omitzero"`
	Result     string `json:"result"`
	IsError    bool   `json:"is_error,omitzero"`
}

func NewToolResultContent(toolCallID, name, result string, isError bool) *ToolResultContent {
	return &ToolResultContent{
		ToolCallID: toolCallID,
		Name:       name,
		Result:     result,
		IsError:    isError,
	}
}

func (t ToolResultContent) Kind() ContentKind {
	return ContentKindToolResult
}

func (t *ToolResultContent) MarshalJSON() ([]byte, error) {
	type alias ToolResultContent
	tmp := struct {
		*alias
		Type ContentKind `json:"type"`
	}{
		alias: (*alias)(t),
		Type:  t.Kind(),
	}
	return json.Marshal(tmp)
}

type DataContent struct {
	MediaType MediaType `json:"media_type"`
	Data      []byte    `json:"data"`
}

func NewDataContent(media MediaType, data []byte) *DataContent {
	return &DataContent{
		MediaType: media,
		Data:      data,
	}
}

func (d DataContent) Kind() ContentKind {
	return ContentKindData
}

func (d *DataContent) MarshalJSON() ([]byte, error) {
	type alias DataContent
	tmp := struct {
		*alias
		Type ContentKind
	}{
		alias: (*alias)(d),
		Type:  d.Kind(),
	}
	return json.Marshal(tmp)
}

// SkillContent marks one skill the caller selected for the turn that carries it. It is a selection
// marker only: before the turn starts, the agent resolves it into a Skill tool call and its tool
// result (see beforeUserInput), so no provider adapter ever renders this content.
type SkillContent struct {
	Name        string `json:"name"`
	Description string `json:"description,omitzero"`
}

func (s SkillContent) Kind() ContentKind {
	return ContentKindSkill
}

func (s *SkillContent) MarshalJSON() ([]byte, error) {
	type alias SkillContent
	tmp := struct {
		*alias
		Type ContentKind `json:"type"`
	}{
		alias: (*alias)(s),
		Type:  s.Kind(),
	}
	return json.Marshal(tmp)
}

func NewUnresolvedSkillContent(name string) *SkillContent {
	return &SkillContent{
		Name: name,
	}
}
