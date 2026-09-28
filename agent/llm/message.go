package llm

type MessageContents []MessageContent

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
	RoleTool      Role = "tool"
)

type Message struct {
	ID       string
	Role     Role
	Contents MessageContents
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
	// ID is the unique identifier of the message, not only the chunk. It means
	// that all chunks of the same message share the same ID.
	ID           string            `json:"id"`
	Type         ResponseChunkType `json:"type"`
	FinishReason string            `json:"finish_reason,omitzero"`
	Role         Role              `json:"role,omitzero"`
	Contents     MessageContents   `json:"contents,omitzero"`
}
