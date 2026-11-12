package event

import "github.com/taichu-lang/raven-agents/agent/llm/underlying"

type SourceType string

const (
	// SourceTypeAgent are events from agent, ex: llm assistant output.
	SourceTypeAgent SourceType = "agent"
	// SourceTypeRuntime are events from runtime system, ex: notification, human-in-the-loop.
	SourceTypeRuntime SourceType = "runtime"
)

type EventName string

const (
	EventTurnStart         EventName = "turn.start"
	EventTurnEnd           EventName = "turn.end"
	EventIterationStart    EventName = "iteration.start"
	EventIterationEnd      EventName = "iteration.end"
	EventLLMStart          EventName = "llm.start"
	EventLLMDelta          EventName = "llm.delta"
	EventLLMReasoningDelta EventName = "llm.reasoning.delta"
	EventLLMUsage          EventName = "llm.usage"
	EventLLMComplete       EventName = "llm.complete"
	EventLLMError          EventName = "llm.error"
	EventHumanApproval     EventName = "runtime.hitl"
	EventRuntimeError      EventName = "runtime.error"
	EventToolCall          EventName = "tool.call"
	EventToolResult        EventName = "tool.result"
	EventToolSearch        EventName = "tool.search"
	EventCompactStart      EventName = "compact.start"
	EventCompactEnd        EventName = "compact.end"
)

// CompactPayload reports one context compaction: the projected context size against the trigger
// threshold, how the view changed, and the running compaction count of the turn. DroppedMessages,
// KeptMessages and SummaryTokens are only set on compact.end.
type CompactPayload struct {
	Projected       uint64 `json:"projected"`
	Threshold       uint64 `json:"threshold"`
	SummaryTokens   int64  `json:"summary_tokens,omitzero"`
	DroppedMessages int    `json:"dropped_messages,omitzero"`
	KeptMessages    int    `json:"kept_messages,omitzero"`
	Compactions     int64  `json:"compactions"`
}

// ToolSearchPayload reports one executed tool search: the query and the names of the tools it resolved to.
type ToolSearchPayload struct {
	Query string   `json:"query"`
	Tools []string `json:"tools"`
}

type Event struct {
	ID      string
	Source  SourceType
	Name    EventName
	Payload interface{}
}

type TurnStartPayload struct {
	ConversationID string                `json:"conversation_id"`
	TurnSeq        int64                 `json:"turn_seq"`
	Model          string                `json:"model"`
	Provider       string                `json:"provider"`
	Messages       []*underlying.Message `json:"messages"`
}

type HumanApprovalScope string

const (
	HumanApprovalScopePermission HumanApprovalScope = "permission"
	HumanApprovalScopeQuestion   HumanApprovalScope = "question"
)

type HumanApprovalPayload struct {
	Scope HumanApprovalScope `json:"scope"`
	Data  interface{}        `json:"data"`
}
