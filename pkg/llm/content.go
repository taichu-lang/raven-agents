package llm

import "encoding/json"

type ContentKind string

const (
	ContentKindText  ContentKind = "text"
	ContentKindURI   ContentKind = "uri"
	ContentKindUsage ContentKind = "usage"
)

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

func (t TextContent) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		TextContent
		Type ContentKind `json:"type"`
	}{
		TextContent: t,
		Type:        ContentKindText,
	})
}

func (t *TextContent) Raw() string {
	return t.Text
}

type URIContent struct {
	MediaType string
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

func (u UsageContent) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		UsageContent
		Type ContentKind `json:"type"`
	}{
		UsageContent: u,
		Type:         ContentKindUsage,
	})
}
