package event

import "github.com/taichu-lang/raven-agents/agent/llm/underlying"

type SourceType string

const (
	// SourceTypeAgent are events from agent, ex: llm assistant output.
	SourceTypeAgent SourceType = "agent"
	// SourceTypeUser are events from user, ex: llm user input.
	SourceTypeUser SourceType = "user"
	// SourceTypeRuntime are events from runtime system, ex: notification, human-in-the-loop.
	SourceTypeRuntime SourceType = "runtime"
)

type EventName string

const (
	EventTurnStart      EventName = "turn.start"
	EventTurnEnd        EventName = "turn.end"
	EventIterationStart EventName = "iteration.start"
	EventIterationEnd   EventName = "iteration.end"
	EventLLMDelta       EventName = "llm.delta"
	EventLLMComplete    EventName = "llm.complete"
	EventLLMError       EventName = "llm.error"
	EventHumanApproval  EventName = "runtime.hitl"
	EventRuntimeError   EventName = "runtime.error"
)

type Event struct {
	ID      string
	Source  SourceType
	Name    EventName
	Payload interface{}
}

type TurnStartPayload struct {
	Model    string
	Provider string
	Input    []*underlying.Message
}

type IterationStartPayload struct {
	Iteration int64
	Input     []*underlying.Message
}
