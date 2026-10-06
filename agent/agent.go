package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
	"uuid"

	"github.com/taichu-lang/raven-agents/agent/hook"
	"github.com/taichu-lang/raven-agents/agent/llm"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/internal/database"
	"github.com/taichu-lang/raven-agents/internal/event"
)

var (
	ErrBudgetExceeded = errors.New("budget exceeded")
)

type AgentOptions struct {
	Name  string
	Store *database.StoreConfig
	LLM   *underlying.ProviderOptions
}

type Agent struct {
	State        *AgentState
	LLM          *llm.Runner
	Budget       Budget
	EventBus     event.Bus
	CtxManager   ContextManager
	ToolRegistry *ToolRegistry
	Hooks        *hook.Registry
	Store        Repository

	// subscription feeds the tracer goroutine; tracerDone is closed once that goroutine has drained
	// the subscription channel and returned.
	subscription *event.Subscription
	tracerDone   chan struct{}
}

func New(eventBus event.Bus, tr *ToolRegistry, options *AgentOptions) *Agent {
	h := &Agent{
		State:        &AgentState{},
		EventBus:     eventBus,
		ToolRegistry: tr,
		Hooks:        hook.NewRegistry(),
	}
	h.init(options)
	return h
}

func (h *Agent) init(options *AgentOptions) {
	if err := options.validate(); err != nil {
		panic(err)
	}

	if store, err := NewRepository(options.Store); err != nil {
		panic(fmt.Errorf("failed to create persistent store, %v", err))
	} else {
		h.Store = store
	}

	h.LLM = llm.NewRunner(options.LLM)

	h.State.Status = StatusIdle
	h.State.StartedAt = time.Now()
	h.State.Model = options.LLM.Model
	h.Budget.MaxIterations = DefaultMaxIterations

	// TODO(Leo): Registry builtin tools.

	// Subscribe synchronously so that no event emitted by Run is missed, and only consume the
	// events in the background.
	tracer := NewTraceSubscriber(options.Name)
	h.subscription = h.EventBus.Subscribe()
	h.tracerDone = make(chan struct{})
	go func() {
		defer close(h.tracerDone)
		for e := range h.subscription.Channel {
			tracer.OnEvent(e)
		}
	}()
}

// Shutdown unsubscribes from the event bus and waits for the tracer goroutine to consume the events
// still buffered in the subscription channel before closing the store.
func (h *Agent) Shutdown(ctx context.Context) error {
	if err := h.EventBus.Unsubscribe(h.subscription.ID); err != nil {
		return err
	}

	select {
	case <-h.tracerDone:
	case <-ctx.Done():
		return ctx.Err()
	}

	return h.Store.Close()
}

// Run is the agent's main loop.
func (h *Agent) Run(ctx context.Context, input []*underlying.Message) error {
	h.State.resetPerTurn()
	h.onEvent(ctx, event.SourceTypeAgent, event.EventTurnStart, &event.TurnStartPayload{
		Model:    h.State.Model,
		Provider: "openai",
		Input:    input,
	})
	defer h.onEvent(ctx, event.SourceTypeAgent, event.EventTurnEnd, nil)

	// Add user message first, as the tool selector might depend on the latest user message.
	h.State.Messages = append(h.State.Messages, input...)
	h.saveMessages(ctx, input)

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
	}

}

func (h *Agent) iterate(
	ctx context.Context,
	opts []underlying.WithGenOption,
) error {
	// tools are returned from the previous iteration, and should be invoked first.
	if len(h.State.ToolCalls) > 0 {
		h.invokeToolCalls(ctx)
	}

	messages, err := h.CtxManager.Build(h.State, h.State.Tools)
	if err != nil {
		return err
	}

	h.onEvent(ctx, event.SourceTypeAgent, event.EventIterationStart, &event.IterationStartPayload{
		Iteration: h.State.Iterations,
		Input:     messages,
	})

	for chunk, err := range h.LLM.Run(ctx, messages, opts...) {
		if err != nil {
			h.onEvent(ctx, event.SourceTypeAgent, event.EventLLMError, err)
			return err
		}

		switch chunk.Type {
		case underlying.ResponseChunkTypeDelta:
			h.onEvent(ctx, event.SourceTypeAgent, event.EventLLMDelta, &chunk.Message)
		case underlying.ResponseChunkTypeFinal:
			// TODO(Leo): if using tools, llm might returns two assistant messages which declare the same
			// tool call. The assistant message id are same in this case. Filter out the duplicate
			// assistant message and tool call content.
			assistant := h.State.onFinalChunk(chunk)
			h.saveMessages(ctx, []*underlying.Message{assistant})
			h.onEvent(ctx, event.SourceTypeAgent, event.EventLLMComplete, assistant)
		case underlying.ResponseChunkTypeUsage:
			usage, _ := chunk.Contents[0].(*underlying.UsageContent)
			if usage != nil {
				h.State.TokenUsage += usage.TotalTokenCount
			}
		}
	}

	h.onEvent(ctx, event.SourceTypeAgent, event.EventIterationEnd, nil)
	return nil
}

func (h *Agent) onEvent(
	ctx context.Context,
	source event.SourceType,
	name event.EventName,
	payload interface{},
) {
	if err := h.EventBus.Emit(ctx, &event.Event{
		ID:      uuid.New().String(),
		Source:  source,
		Name:    name,
		Payload: payload,
	}); err != nil {
		slog.Warn("failed to emit event", "name", name, "err", err)
	}
}

func (h *Agent) saveMessages(ctx context.Context, messages []*underlying.Message) {
	err := h.Store.StoreMessages(ctx, h.State.Model, messages)
	if err != nil {
		slog.Warn("failed to persistent messages into store", "err", err)
		h.onEvent(ctx, event.SourceTypeAgent, event.EventRuntimeError, err)
	}
}

func (h *Agent) invokeToolCalls(ctx context.Context) {
	if continueInvoke, err := h.beforeToolCall(ctx); err != nil || !continueInvoke {
		return
	}

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

// beforeToolCall invokes hooks before tool calls, returns whether to continue invoking tool calls.
func (h *Agent) beforeToolCall(ctx context.Context) (bool, error) {
	result := &underlying.Message{Role: underlying.RoleTool}
	tools := h.State.ToolCalls
	h.State.ToolCalls = tools[:0]
	for _, call := range tools {
		output, err := h.Hooks.Run(ctx, &hook.Input{
			Event: hook.EventPreToolUse,
			Tool: &hook.ToolInput{
				Name:  call.Name,
				Input: call.Arguments,
			},
		})
		if err != nil {
			return false, err
		}

		switch output.Decision {
		case hook.PermissionDeny:
			// Send back to llm, and let llm to decide whether to change the tool call (ex: use another tool,
			// or update the arguments).
			result.Contents = append(
				result.Contents,
				underlying.NewToolResultContent(call.ID, call.Name, "denied: "+output.Reason, false),
			)

		case hook.PermissionAsk:
			h.State.Status = StatusWaitingApproval
			h.State.Pending = append(h.State.Pending, &PendingApproval{
				ToolCall:    call,
				Reason:      output.Reason,
				RequestedAt: time.Now(),
			})

		case hook.PermissionAllow:
			if output.Tool != nil && output.Tool.UpdatedArgs != nil {
				call.Arguments = *output.Tool.UpdatedArgs
			}

			// TODO(Leo): Would this disrupt the order of the LLM's output?
			h.State.ToolCalls = append(h.State.ToolCalls, call)
		}

	}

	continueInvokeCalls := true

	if len(h.State.Pending) > 0 {
		h.onEvent(ctx, event.SourceTypeRuntime, event.EventHumanApproval, h.State.Pending)
		continueInvokeCalls = false
	}

	if len(result.Contents) > 0 {
		h.State.Messages = append(h.State.Messages, result)
	}

	return continueInvokeCalls, nil
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

func (o *AgentOptions) validate() error {
	if o.Name == "" {
		return errors.New("name is required")
	}

	if o.LLM == nil {
		return errors.New("llm options is required")
	}

	if err := o.LLM.Validate(); err != nil {
		return err
	}

	if o.Store != nil {
		if err := o.Store.Validate(); err != nil {
			return err
		}
	}

	return nil
}
