package event

import "github.com/taichu-lang/raven-agents/agent/llm/underlying"

type SourceType string

const (
	SourceTypeAgent       SourceType = "agent"
	SourceTypeUser        SourceType = "user"
	SourceTypeEnvironment SourceType = "environment"
	SourceTypeHook        SourceType = "hook"
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
