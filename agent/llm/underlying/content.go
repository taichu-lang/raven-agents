package underlying

import (
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

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

	// ContentKindReasoning carries the reasoning of a reasoning model. How it reaches the model
	// is left to each provider, as the shape of the request differs between model apis.
	ContentKindReasoning ContentKind = "reasoning"

	// ContentKindSkill discloses a skill to the model. How it reaches the model is left to each
	// provider, as the shape of the request differs between model apis.
	ContentKindSkill ContentKind = "skill"

	// ContentKindToolSearchCall carries a tool search issued by the model. How it reaches the
	// model is left to each provider (ex: OpenAI renders it as a tool_search_call item).
	ContentKindToolSearchCall ContentKind = "tool_search_call"

	// ContentKindToolSearchResult carries the tools a search resolved to. How it reaches the
	// model is left to each provider (ex: OpenAI renders it as a tool_search_output item).
	ContentKindToolSearchResult ContentKind = "tool_search_result"
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
	Namespace string `json:"namespace,omitzero"`
	Arguments string `json:"arguments"`
}

func NewToolCallContent(id, name, namespace, arguments string) *ToolCallContent {
	return &ToolCallContent{
		ID:        id,
		Name:      name,
		Namespace: namespace,
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

// ReasoningContent carries the reasoning produced by a reasoning model. Summary holds the
// human-readable reasoning summary for display only (ex: the "thinking" section of the UI,
// including history replay); it is never sent back to the provider. EncryptedContent is the
// base64-encoded, provider-encrypted reasoning payload; it must be preserved and sent back on
// subsequent turns so the model keeps its reasoning context (ex: stateless mode or ZDR).
type ReasoningContent struct {
	ID               string `json:"id,omitzero"`
	Summary          string `json:"summary,omitzero"`
	EncryptedContent string `json:"encrypted_content,omitzero"`
}

func NewReasoningContent(id, summary string) *ReasoningContent {
	return &ReasoningContent{
		ID:      id,
		Summary: summary,
	}
}

func (r ReasoningContent) Kind() ContentKind {
	return ContentKindReasoning
}

func (r *ReasoningContent) MarshalJSON() ([]byte, error) {
	type alias ReasoningContent
	tmp := struct {
		*alias
		Type ContentKind `json:"type"`
	}{
		alias: (*alias)(r),
		Type:  r.Kind(),
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
		Type ContentKind `json:"type"`
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

// ToolDefinition is the serializable definition of a tool carried by a tool search result, so
// the model sees the full shape (name, description, input schema) of a discovered tool.
type ToolDefinition struct {
	Name string `json:"name"`
	// Namespace is required for OpenAI.
	Namespace   string             `json:"namespace,omitzero"`
	Description string             `json:"description,omitzero"`
	InputSchema *jsonschema.Schema `json:"input_schema,omitzero"`
}

// ToolSearchCallContent is a search the model issued, living in an assistant message. Arguments
// is kept raw: the agent parses query/limit against the schema it advertised, so a schema change
// never touches the message layer or the persisted format.
type ToolSearchCallContent struct {
	ID        string          `json:"id,omitzero"`
	CallID    string          `json:"call_id"`
	Arguments json.RawMessage `json:"arguments"`
}

func NewToolSearchCallContent(id, callID string, arguments json.RawMessage) *ToolSearchCallContent {
	return &ToolSearchCallContent{
		ID:        id,
		CallID:    callID,
		Arguments: arguments,
	}
}

func (t ToolSearchCallContent) Kind() ContentKind {
	return ContentKindToolSearchCall
}

func (t *ToolSearchCallContent) MarshalJSON() ([]byte, error) {
	type alias ToolSearchCallContent
	tmp := struct {
		*alias
		Type ContentKind `json:"type"`
	}{
		alias: (*alias)(t),
		Type:  t.Kind(),
	}
	return json.Marshal(tmp)
}

// ToolSearchResultContent is the result of a tool search, living in a RoleTool message next to
// ToolResultContent. It carries the full definitions of the tools the search resolved to.
type ToolSearchResultContent struct {
	CallID string            `json:"call_id"`
	Tools  []*ToolDefinition `json:"tools"`
}

func NewToolSearchResultContent(callID string, tools []*ToolDefinition) *ToolSearchResultContent {
	return &ToolSearchResultContent{
		CallID: callID,
		Tools:  tools,
	}
}

func (t ToolSearchResultContent) Kind() ContentKind {
	return ContentKindToolSearchResult
}

func (t *ToolSearchResultContent) MarshalJSON() ([]byte, error) {
	type alias ToolSearchResultContent
	tmp := struct {
		*alias
		Type ContentKind `json:"type"`
	}{
		alias: (*alias)(t),
		Type:  t.Kind(),
	}
	return json.Marshal(tmp)
}

// UnmarshalMessageContents decodes the JSON produced by marshalling a MessageContents. It is the
// inverse of the per type MarshalJSON methods: every content carries the type discriminator they
// write next to their own fields.
func UnmarshalMessageContents(data []byte) (MessageContents, error) {
	if len(data) == 0 {
		return MessageContents{}, nil
	}

	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("unmarshal message contents: %w", err)
	}

	contents := make(MessageContents, 0, len(items))
	for _, item := range items {
		content, err := unmarshalMessageContent(item)
		if err != nil {
			return nil, err
		}

		contents = append(contents, content)
	}

	return contents, nil
}

func unmarshalMessageContent(data json.RawMessage) (MessageContent, error) {
	var discriminator struct {
		Type ContentKind `json:"type"`
	}
	if err := json.Unmarshal(data, &discriminator); err != nil {
		return nil, fmt.Errorf("unmarshal message content: %w", err)
	}

	var content MessageContent
	switch discriminator.Type {
	case ContentKindText:
		content = new(TextContent)
	case ContentKindUsage:
		content = new(UsageContent)
	case ContentKindData:
		content = new(DataContent)
	case ContentKindToolCall:
		content = new(ToolCallContent)
	case ContentKindToolResult:
		content = new(ToolResultContent)
	case ContentKindReasoning:
		content = new(ReasoningContent)
	case ContentKindSkill:
		content = new(SkillContent)
	case ContentKindToolSearchCall:
		content = new(ToolSearchCallContent)
	case ContentKindToolSearchResult:
		content = new(ToolSearchResultContent)
	default:
		return nil, fmt.Errorf("unmarshal message content: unknown type %q", discriminator.Type)
	}

	if err := json.Unmarshal(data, content); err != nil {
		return nil, fmt.Errorf("unmarshal %s content: %w", discriminator.Type, err)
	}

	return content, nil
}
