package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/taichu-lang/raven-agents/agent/hook"
	"github.com/taichu-lang/raven-agents/agent/llm"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/agent/persistent"
	"github.com/taichu-lang/raven-agents/internal/database"
	"github.com/taichu-lang/raven-agents/internal/event"
	"github.com/uptrace/bun"
)

var (
	ErrBudgetExceeded = errors.New("budget exceeded")
)

type AgentOptions struct {
	ConversationID string
	UserID         string
	Name           string
	LLM            *underlying.ProviderOptions
	SkillDirs      []string

	// Persistent store configuration, either creating a new database connection or using the shared one.
	StoreCfg *database.StoreConfig
	Store    *bun.DB
}

type Agent struct {
	State         *AgentState
	LLM           *llm.Runner
	Budget        Budget
	EventBus      event.Bus
	SkillRegistry *SkillRegistry
	ToolRegistry  *ToolRegistry
	Hooks         *hook.Registry
	Store         Repository

	// subscription feeds the tracer goroutine; tracerDone is closed once that goroutine has drained
	// the subscription channel and returned.
	subscription *event.Subscription
	tracerDone   chan struct{}
	tracer       *TraceSubscriber
}

func New(eventBus event.Bus, tr *ToolRegistry, options *AgentOptions) *Agent {
	if err := options.validate(); err != nil {
		panic(err)
	}

	a := &Agent{
		State: &AgentState{
			Status:         persistent.TurnStatusIdle,
			StartedAt:      time.Now(),
			Model:          options.LLM.Model,
			UserID:         options.UserID,
			ConversationID: options.ConversationID,
		},
		EventBus:     eventBus,
		ToolRegistry: tr,
		Hooks:        hook.NewRegistry(),
	}
	a.init(options)
	return a
}

func (a *Agent) init(options *AgentOptions) {
	if store, err := NewRepository(options.Store, options.StoreCfg); err != nil {
		panic(fmt.Errorf("failed to create persistent store, %v", err))
	} else {
		a.Store = store
	}

	// As the Agent instance can be used in multi-turn conversation, ex: keep the Agent instance
	// alive in a RunManager in a standalone environment, restore the previous turns here rather
	// than before each turn.
	if err := a.restore(options.ConversationID); err != nil {
		panic(fmt.Errorf("failed to restore messages, %v", err))
	}

	a.LLM = llm.NewRunner(options.LLM)
	a.Budget.MaxIterations = DefaultMaxIterations
	a.SkillRegistry = NewSkillRegistry(options.SkillDirs)
	loadSkillTool := NewSkillTool(a.SkillRegistry)

	// The active set is computed once per turn, so the tool stays available for the whole
	// turn as long as any active skill is installed.
	a.ToolRegistry.Register(loadSkillTool, func(state *AgentState) bool {
		return len(a.SkillRegistry.Available(state)) > 0
	})

	// Subscribe synchronously so that no event emitted by Run is missed, and only consume the
	// events in the background.
	a.tracer = NewTraceSubscriber(options.Name, options.UserID)
	a.subscription = a.EventBus.Subscribe()
	a.tracerDone = make(chan struct{})
	go func() {
		defer close(a.tracerDone)
		for e := range a.subscription.Channel {
			a.tracer.OnEvent(e)
		}
	}()
}

// Shutdown unsubscribes from the event bus and waits for the tracer goroutine to consume the events
// still buffered in the subscription channel before closing the store.
func (a *Agent) Shutdown(ctx context.Context) error {
	defer a.tracer.Close()
	if err := a.EventBus.Unsubscribe(a.subscription.ID); err != nil {
		return err
	}

	select {
	case <-a.tracerDone:
	case <-ctx.Done():
		return ctx.Err()
	}

	return a.Store.Close()
}

// Run is the agent's main loop.
func (a *Agent) Run(ctx context.Context, input []*underlying.Message) error {
	a.State.resetPerTurn()
	messages, err := a.beforeUserInput(ctx, input)
	if err != nil {
		return err
	}

	turn, err := a.Store.AddTurn(ctx, &a.State.TurnContext, messages[0].ID)
	if err != nil {
		return err
	}

	a.onEvent(ctx, event.SourceTypeAgent, event.EventTurnStart, &event.TurnStartPayload{
		Turn:     turn.TurnSeq,
		Model:    a.State.Model,
		Provider: "openai",
		Input:    input,
	})
	defer func() {
		_ = a.Store.UpdateTurn(ctx, &a.State.TurnContext)
		a.onEvent(ctx, event.SourceTypeAgent, event.EventTurnEnd, nil)
	}()

	// Add user message first, as the tool selector might depend on the latest user message.
	a.State.Messages = append(a.State.Messages, messages...)
	a.saveMessages(ctx, messages)

	// The system and tools prompt should keep immutable during the turn.
	a.State.Tools = a.ToolRegistry.Active(a.State)
	opts := make([]underlying.WithGenOption, 0, len(a.State.Tools))
	for _, t := range a.State.Tools {
		opts = append(opts, underlying.WithTool(t))
	}

	for {
		if a.Budget.Exceeded(a.State) {
			a.State.Status = persistent.TurnStatusInterrupted
			return ErrBudgetExceeded
		}

		a.State.Iterations++
		if err := a.iterate(ctx, opts); err != nil {
			a.State.Status = persistent.TurnStatusFailed
			return err
		}

		if len(a.State.ToolCalls) == 0 {
			a.State.Status = persistent.TurnStatusDone
			return nil
		}
	}
}

func (a *Agent) iterate(
	ctx context.Context,
	opts []underlying.WithGenOption,
) error {
	// tools are returned from the previous iteration, and should be invoked first.
	if len(a.State.ToolCalls) > 0 {
		a.invokeToolCalls(ctx)
	}

	messages := a.State.Messages

	a.onEvent(ctx, event.SourceTypeAgent, event.EventIterationStart, &event.IterationStartPayload{
		Iteration: a.State.Iterations,
		Input:     messages,
	})

	for chunk, err := range a.LLM.Run(ctx, messages, opts...) {
		if err != nil {
			a.onEvent(ctx, event.SourceTypeAgent, event.EventLLMError, err)
			return err
		}

		switch chunk.Type {
		case underlying.ResponseChunkTypeDelta:
			a.onEvent(ctx, event.SourceTypeAgent, event.EventLLMDelta, &chunk.Message)
		case underlying.ResponseChunkTypeFinal:
			// TODO(Leo): if using tools, llm might returns two assistant messages which declare the same
			// tool call. The assistant message id are same in this case. Filter out the duplicate
			// assistant message and tool call content.
			assistant := a.State.onFinalChunk(chunk)
			a.saveMessages(ctx, []*underlying.Message{assistant})
			a.onEvent(ctx, event.SourceTypeAgent, event.EventLLMComplete, assistant)
		case underlying.ResponseChunkTypeUsage:
			usage, _ := chunk.Contents[0].(*underlying.UsageContent)
			if usage != nil {
				a.State.TokenUsage += usage.TotalTokenCount
			}
		}
	}

	a.onEvent(ctx, event.SourceTypeAgent, event.EventIterationEnd, nil)
	return nil
}

func (a *Agent) onEvent(
	ctx context.Context,
	source event.SourceType,
	name event.EventName,
	payload interface{},
) {
	if err := a.EventBus.Emit(ctx, &event.Event{
		ID:      uuid.NewString(),
		Source:  source,
		Name:    name,
		Payload: payload,
	}); err != nil {
		slog.Warn("failed to emit event", "name", name, "err", err)
	}
}

func (a *Agent) saveMessages(ctx context.Context, messages []*underlying.Message) {
	err := a.Store.StoreMessages(ctx, &a.State.TurnContext, messages)
	if err != nil {
		slog.Warn("failed to persistent messages into store", "err", err)
		a.onEvent(ctx, event.SourceTypeAgent, event.EventRuntimeError, err)
	}
}

func (a *Agent) invokeToolCalls(ctx context.Context) {
	if continueInvoke, err := a.beforeToolCall(ctx); err != nil || !continueInvoke {
		return
	}

	result := &underlying.Message{Role: underlying.RoleTool}
	tools := a.State.ToolCalls
	if len(tools) == 1 {
		result.Contents = append(result.Contents, a.invokeOneTool(ctx, tools[0]))
	} else {
		var mu sync.Mutex
		var wg sync.WaitGroup
		wg.Add(len(tools))
		for _, call := range tools {
			go func() {
				defer wg.Done()
				content := a.invokeOneTool(ctx, call)
				mu.Lock()
				defer mu.Unlock()
				// TODO(Leo): The tool results array should be sorted to match the order of tool IDs in the
				// tool calls array.
				result.Contents = append(result.Contents, content)
			}()
		}
		wg.Wait()
	}

	a.recordActivatedSkills(tools, result)

	a.State.Messages = append(a.State.Messages, result)
	a.State.ToolCalls = tools[:0]
}

// recordActivatedSkills notes which skills the model pulled in with the skill tool, so that they
// are no longer offered in the catalog. It runs once the concurrent invocations have been joined,
// which is what keeps the state written from a single goroutine.
func (a *Agent) recordActivatedSkills(
	calls []*underlying.ToolCallContent,
	result *underlying.Message,
) {
	succeeded := make(map[string]struct{}, len(result.Contents))
	for _, content := range result.Contents {
		if out, ok := content.(*underlying.ToolResultContent); ok && !out.IsError {
			succeeded[out.ToolCallID] = struct{}{}
		}
	}

	for _, call := range calls {
		if call.Name != SkillToolName {
			continue
		}

		if _, ok := succeeded[call.ID]; !ok {
			continue
		}

		var in skillToolInput
		if err := json.Unmarshal([]byte(call.Arguments), &in); err != nil {
			slog.Warn("failed to read back the name of an activated skill", "err", err)
			continue
		}

		a.State.activateSkill(in.Name)
	}
}

func (a *Agent) invokeOneTool(
	ctx context.Context,
	call *underlying.ToolCallContent,
) *underlying.ToolResultContent {
	t := a.ToolRegistry.Get(call.Name)
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

	if o.UserID == "" {
		return errors.New("user_id is required")
	}

	if o.ConversationID == "" {
		return errors.New("conversation_id is required")
	}

	if o.LLM == nil {
		return errors.New("llm options is required")
	}

	if err := o.LLM.Validate(); err != nil {
		return err
	}

	if o.StoreCfg != nil {
		if err := o.StoreCfg.Validate(); err != nil {
			return err
		}
	}

	return nil
}
