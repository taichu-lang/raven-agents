package llm

import "encoding/json"

type ContentKind string

const (
	ContentKindText  ContentKind = "text"
	ContentKindURI   ContentKind = "uri"
	ContentKindUsage ContentKind = "usage"

	// ContentKindData is used to store binary and non-binary data, which depends on the media type.
	// Ex: an image file, a text file, etc.
	ContentKindData ContentKind = "data"
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

func (t *TextContent) Raw() string {
	return t.Text
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
		Type ContentKind
	}{
		alias: (*alias)(u),
		Type:  u.Kind(),
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
