package hook

import "context"

type Event string

const (
	// EventUserInput invokes when a user message is accepted, before sending to llm. Ex: give a chance to
	// check for sensitive words or biases.
	EventUserInput   Event = "user_input"
	EventPreToolUse  Event = "pre_tool_use"
	EventPostToolUse Event = "post_tool_use"
)

type Permission string

const (
	PermissionAllow Permission = "allow"
	PermissionDeny  Permission = "deny"
	PermissionAsk   Permission = "ask"
)

type ToolInput struct {
	Name  string `json:"name"`
	Input string `json:"input"`
}

type Input struct {
	Event Event
	Tool  *ToolInput
}

type ToolOutput struct {
	UpdatedArgs *string
}

type Output struct {
	Decision Permission
	Reason   string
	Tool     *ToolOutput
}

type HookHandler func(ctx context.Context, in *Input) (*Output, error)
