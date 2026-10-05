package agent

import (
	"time"

	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/tool"
)

type Status string

const (
	StatusIdle            Status = "idle"
	StatusRunning         Status = "running"
	StatusWaitingApproval Status = "waiting_approval"
	StatusDone            Status = "done"
	StatusFailed          Status = "failed"
)

type PendingApproval struct {
	ToolCall    *underlying.ToolCallContent
	Reason      string `json:"reason,omitzero"`
	RequestedAt time.Time
}

// AgentState represents the shared state of the agent, which can be accessed and modified by all nodes in the
// graph. Fields in this struct are intended to be persistent, and can be resumed after a restart of the agent.
type AgentState struct {
	Status   Status
	Messages []*underlying.Message

	// Tools is the list of active tools in each turn.
	Tools []tool.FuncTool

	// ToolCalls is the list of tools returned from llm, and to be called from agent.
	ToolCalls []*underlying.ToolCallContent

	// Pending is the list of actions waiting for human approval.
	Pending []*PendingApproval

	// TODO(Leo): Move those statistics to a RunContext?
	StartedAt  time.Time
	Iterations int64
	TokenUsage int64
	TokenCost  float64
	Model      string
}

func (s *AgentState) resetPerTurn() {
	s.Iterations = 0
	s.TokenUsage = 0
	s.TokenCost = 0
	s.StartedAt = time.Now()
}

func (s *AgentState) lastMessage() *underlying.Message {
	if len(s.Messages) == 0 {
		return nil
	}

	return s.Messages[len(s.Messages)-1]
}

func (s *AgentState) lastUserMessage() *underlying.Message {
	if s.lastMessage() == nil {
		return nil
	}

	idx := len(s.Messages) - 1
	for idx >= 0 {
		if s.Messages[idx].Role == underlying.RoleUser {
			return s.Messages[idx]
		}
		idx--
	}

	return nil
}

func (s *AgentState) onFinalChunk(chunk *underlying.ResponseChunk) *underlying.Message {
	assistant := &underlying.Message{
		ID:       chunk.ID,
		Role:     chunk.Role,
		Contents: chunk.Contents,
	}
	s.Messages = append(s.Messages, assistant)

	for _, content := range chunk.Contents {
		if call, ok := content.(*underlying.ToolCallContent); ok {
			s.ToolCalls = append(s.ToolCalls, call)
		}
	}

	return assistant
}

const (
	DefaultMaxIterations int64 = 500
)

// Budget represents the resource limits for the agent, which can be used to control the execution of the agent
// and prevent it from consuming too many resources.
type Budget struct {
	// MaxIterations is the maximum number of loop iterations in each turn, to prevent infinite loops.
	MaxIterations int64

	// MaxContextTokens is the maximum number of tokens in the context window, used to trigger context compaction.
	MaxContextTokens int64

	// MaxDuration is the maximum duration of the agent execution, in seconds, to prevent long-running.
	MaxDuration int64

	// MaxTotalTokens is the maximum number of tokens in the entire execution.
	MaxTotalTokens int64

	// MaxCost is the maximum cost of the agent execution, in dollars, to prevent overspending.
	MaxCost float64
}

func (b Budget) Exceeded(s *AgentState) bool {
	if b.MaxIterations > 0 && s.Iterations >= b.MaxIterations {
		return true
	}

	if b.MaxTotalTokens > 0 && s.TokenUsage >= b.MaxTotalTokens {
		return true
	}

	if b.MaxCost > 0 && s.TokenCost >= b.MaxCost {
		return true
	}

	if b.MaxDuration > 0 && time.Since(s.StartedAt).Seconds() >= float64(b.MaxDuration) {
		return true
	}

	return false
}
