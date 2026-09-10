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

type ResponseChunk struct {
	FinishReason string          `json:"finish_reason,omitzero"`
	Role         Role            `json:"role,omitzero"`
	Contents     MessageContents `json:"contents,omitzero"`
}
