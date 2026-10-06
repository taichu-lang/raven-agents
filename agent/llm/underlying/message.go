package underlying

import "strings"

type MessageContents []MessageContent

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
	RoleTool      Role = "tool"
)

type Message struct {
	ID       string          `json:"id"`
	Role     Role            `json:"role"`
	Contents MessageContents `json:"contents,omitzero"`
}

// FinishReasonDone represents that the response is completed successfully.
const FinishReasonDone = "done"

type ResponseChunkType string

const (
	ResponseChunkTypeDelta ResponseChunkType = "delta"
	ResponseChunkTypeFinal ResponseChunkType = "final"
	ResponseChunkTypeUsage ResponseChunkType = "usage"
)

type ResponseChunk struct {
	Message
	Type         ResponseChunkType `json:"type"`
	FinishReason string            `json:"finish_reason,omitzero"`
}

func (c MessageContents) CollectText() string {
	sb := strings.Builder{}
	for _, content := range c {
		if textContent, ok := content.(*TextContent); ok {
			sb.WriteString(textContent.Text)
		}
	}

	return sb.String()
}
