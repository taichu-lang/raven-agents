package underlying

import (
	"strings"

	"github.com/google/uuid"
)

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
	ResponseChunkTypeReasoningDelta ResponseChunkType = "reasoning.delta"
	ResponseChunkTypeDelta          ResponseChunkType = "delta"
	ResponseChunkTypeFinal          ResponseChunkType = "final"
	ResponseChunkTypeUsage          ResponseChunkType = "usage"

	// ResponseChunkTypeSummary marks the final chunk of a context compaction call, re-tagged by the
	// SummaryTagMiddleware so that middlewares on the shared chain can attribute the call (and its usage) to
	// compaction. The agent loop never sees this type: the compaction stream is consumed inside the compactor.
	ResponseChunkTypeSummary ResponseChunkType = "summary"
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

func NewMessage(role Role, contents MessageContents) *Message {
	return &Message{
		ID:       uuid.NewString(),
		Role:     role,
		Contents: contents,
	}
}
