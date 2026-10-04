package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
	"uuid"

	"github.com/taichu-lang/raven-agents/agent/llm"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/internal/event"
)

var (
	ErrBudgetExceeded = errors.New("budget exceeded")
)

type Agent struct {
	State        *AgentState
	Budget       Budget
	EventBus     event.Bus
	LLM          *llm.Runner
	CtxManager   ContextManager
	ToolRegistry *ToolRegistry
}

func New(eventBus event.Bus, runner *llm.Runner, tr *ToolRegistry) *Agent {
	h := &Agent{
		State:        &AgentState{},
		LLM:          runner,
		EventBus:     eventBus,
		ToolRegistry: tr,
	}
	h.init()
	return h
}

func (h *Agent) init() {
	h.State.Status = StatusIdle
	h.State.StartedAt = time.Now()
	h.Budget.MaxIterations = DefaultMaxIterations

	// TODO(Leo): Registry builtin tools.

	tracer := NewTraceSubscriber("some-agent")
	h.EventBus.Subscribe(tracer.OnEvent)
}

// Run is the agent's main loop.
func (h *Agent) Run(ctx context.Context, model string, input []*underlying.Message) error {
	h.State.resetPerTurn()
	h.onEvent(event.SourceTypeAgent, event.EventTurnStart, &event.TurnStartPayload{
		Model:    model,
		Provider: "openai",
		Input:    input,
	})
	defer h.onEvent(event.SourceTypeAgent, event.EventTurnEnd, nil)

	// Add user message first, as the tool selector might depend on the latest user message.
	h.State.Messages = append(h.State.Messages, input...)
	h.State.Tools = h.ToolRegistry.Active(h.State)
	opts := make([]underlying.WithGenOption, 0, len(h.State.Tools))
	for _, t := range h.State.Tools {
		opts = append(opts, underlying.WithTool(t))
	}

	for {
		if h.Budget.Exceeded(h.State) {
			return ErrBudgetExceeded
		}

		h.State.Iterations++
		if err := h.iterate(ctx, opts); err != nil {
			return err
		}

		if len(h.State.ToolCalls) == 0 {
			return nil
		}

		h.invokeToolCalls(ctx)
	}

}

func (h *Agent) iterate(ctx context.Context, opts []underlying.WithGenOption) error {
	messages, err := h.CtxManager.Build(h.State, h.State.Tools)
	if err != nil {
		return err
	}

	h.onEvent(event.SourceTypeAgent, event.EventIterationStart, &event.IterationStartPayload{
		Iteration: h.State.Iterations,
		Input:     messages,
	})

	for chunk, err := range h.LLM.Run(ctx, messages, opts...) {
		if err != nil {
			h.onEvent(event.SourceTypeAgent, event.EventLLMError, err)
			return err
		}

		switch chunk.Type {
		case underlying.ResponseChunkTypeDelta:
			h.onEvent(event.SourceTypeAgent, event.EventLLMDelta, chunk.Contents)
		case underlying.ResponseChunkTypeFinal:
			// TODO(Leo): if using tools, llm might returns two assistant messages which declare the same
			// tool call. The assistant message id are same in this case. Filter out the duplicate
			// assistant message and tool call content.
			h.onEvent(event.SourceTypeAgent, event.EventLLMComplete, chunk.Contents)
			h.State.onFinalChunk(chunk)
		case underlying.ResponseChunkTypeUsage:
			usage, _ := chunk.Contents[0].(*underlying.UsageContent)
			if usage != nil {
				h.State.TokenUsage += usage.TotalTokenCount
			}
		}
	}

	h.onEvent(event.SourceTypeAgent, event.EventIterationEnd, nil)
	return nil
}

func (h *Agent) onEvent(source event.SourceType, name event.EventName, payload interface{}) {
	h.EventBus.Emit(&event.Event{
		ID:      uuid.New().String(),
		Source:  source,
		Name:    name,
		Payload: payload,
	})
}

func (h *Agent) invokeToolCalls(ctx context.Context) {
	result := &underlying.Message{Role: underlying.RoleTool}

	tools := h.State.ToolCalls
	if len(tools) == 1 {
		result.Contents = append(result.Contents, h.invokeOneTool(ctx, tools[0]))
	} else {
		var mu sync.Mutex
		var wg sync.WaitGroup
		wg.Add(len(tools))
		for _, call := range tools {
			go func() {
				defer wg.Done()
				content := h.invokeOneTool(ctx, call)
				mu.Lock()
				defer mu.Unlock()
				// TODO(Leo): The tool results array should be sorted to match the order of tool IDs in the
				// tool calls array.
				result.Contents = append(result.Contents, content)
			}()
		}
		wg.Wait()
	}

	h.State.Messages = append(h.State.Messages, result)
	h.State.ToolCalls = tools[:0]
}

func (h *Agent) invokeOneTool(
	ctx context.Context,
	call *underlying.ToolCallContent,
) *underlying.ToolResultContent {
	t := h.ToolRegistry.Get(call.Name)
	if t == nil {
		return underlying.NewToolResultContent(
			call.ID,
			call.Name,
			fmt.Sprintf("tool %q not found", call.Name),
			true,
		)
	}

	out, err := t.Call(ctx, call.Arguments)
	if err != nil {
		return underlying.NewToolResultContent(call.ID, call.Name, err.Error(), true)
	}

	encoded, err := json.Marshal(out)
	if err != nil {
		return underlying.NewToolResultContent(call.ID, call.Name, err.Error(), true)
	}

	return underlying.NewToolResultContent(call.ID, call.Name, string(encoded), false)
}
