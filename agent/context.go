package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/taichu-lang/raven-agents/agent/llm"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/agent/persistent"
	"github.com/taichu-lang/raven-agents/pkg/database"
	"github.com/taichu-lang/raven-agents/pkg/event"
)

type ContextManager struct {
	Messages []*underlying.Message

	store     Repository
	compactor *Compactor

	// catalog is the model catalog of the provider; its context window and output limit derive the compaction
	// threshold.
	catalog *underlying.Catalog

	// contextWindowUsed is the provider-reported input size of the most recent llm call, the only ground truth
	// for the current context size.
	contextWindowUsed uint64

	// contextCursor is the cursor of the last message when the contextWindowUsed was set, i.e.: len(Messages).
	contextCursor int
}

func NewContextManager(
	provider *underlying.ProviderOptions,
	compact *CompactOptions,
	middlewares []llm.Middleware,
	store Repository,
) *ContextManager {
	cm := &ContextManager{
		store: store,
	}
	cm.compactor = NewCompactor(provider, compact, middlewares...)
	cm.catalog = provider.Catalog

	return cm
}

// compactThreshold resolves the context size at which compaction triggers: the smaller of the catalog window
// times the safety factor and the window minus the model's maximum output, so the reply of the call that
// follows a compaction check always fits. Zero disables compaction.
func (c *ContextManager) compactThreshold() uint64 {
	window := c.catalog.Limit.Context
	if window == 0 {
		return 0
	}

	threshold := uint64(float64(window) * c.compactor.options.SafetyFactor)

	// An output limit that is not smaller than the window carries no usable reserve information.
	maxOutput := c.catalog.Limit.Output
	if maxOutput > 0 && maxOutput < window {
		threshold = min(threshold, window-maxOutput)
	}

	return threshold
}

// maybeCompact is Stage 3 (Context Build) of the iteration: when the projected context approaches
// the window, fold the oldest messages into a persisted summary so the LLM call of this iteration
// sees a view that fits. Summarization failures fail open — the threshold is heuristic and the
// provider may still accept the un-compacted call.
func (c *ContextManager) CompactIfNeed(
	ctx context.Context,
	state *AgentState,
	onEvent func(context.Context, event.SourceType, event.EventName, interface{}),
) (*CompactResult, error) {
	threshold := c.compactThreshold()
	if threshold <= 0 {
		slog.Warn("context compaction disabled, can not get window size")
		return nil, nil
	}

	projected := c.contextWindowUsed + estimateTokens(c.Messages[c.contextCursor:])
	if projected < threshold {
		return nil, nil
	}

	if state.Compactions >= c.compactor.options.MaxPerTurn {
		return nil, ErrCompactionExhausted
	}

	onEvent(ctx, event.SourceTypeAgent, event.EventCompactStart, &event.CompactPayload{
		Projected:   projected,
		Threshold:   threshold,
		Compactions: state.Compactions,
	})

	result, err := c.compactor.Compact(ctx, c.Messages)
	if err != nil {
		// Since the compression threshold is just an estimate, even if compression fails here, we can still
		// proceed with the LLM API call and trust the model's output.
		slog.Warn("context compaction failed, continuing un-compacted", "err", err)
		onEvent(ctx, event.SourceTypeAgent, event.EventRuntimeError, err)
		return nil, nil
	}

	if result == nil {
		return nil, nil
	}

	if estimateTokens(result.View) >= projected {
		slog.Warn("context compaction did not shrink the view, discarding", "projected", projected)
		return nil, nil
	}

	// The in-memory view is already compacted, the database error does not affect this turn, so warn and
	// continue.
	if err := c.store.StoreSummary(ctx, &state.TurnContext, result.Row); err != nil {
		slog.Warn("failed to persist compaction summary", "err", err)
		onEvent(ctx, event.SourceTypeAgent, event.EventRuntimeError, err)
	}

	c.Messages = result.View

	// The measurement baseline is gone with the old view: the next check estimates the whole new view until
	// fresh usage arrives.
	c.contextWindowUsed, c.contextCursor = 0, 0
	state.Compactions++

	payload := &event.CompactPayload{
		Projected:       projected,
		Threshold:       threshold,
		DroppedMessages: result.Dropped,
		KeptMessages:    len(result.View),
		Compactions:     state.Compactions,
	}
	if result.Usage != nil {
		payload.SummaryTokens = result.Usage.Total
	}

	onEvent(ctx, event.SourceTypeAgent, event.EventCompactEnd, payload)
	return result, nil
}

func (c *ContextManager) OnUsage(usage *underlying.UsageContent) {
	c.contextWindowUsed = uint64(usage.Input)
	c.contextCursor = len(c.Messages)
}

func (c *ContextManager) Restore(ctx context.Context, conversationID string) error {
	summary, err := c.store.LoadLatestSummary(ctx, conversationID)
	if err != nil {
		slog.Error("database error", "err", err)
		return err
	}

	var messages []*underlying.Message
	if summary == nil {
		messages, err = c.store.LoadMessages(ctx, conversationID)
	} else {
		messages, err = c.loadCompactedView(ctx, conversationID, summary)
	}

	if err != nil {
		return err
	}

	if len(messages) > 0 {
		c.Messages = append(c.Messages, messages...)
	}

	return nil
}

func (a *Agent) restoreIfPresent(conversationID string) error {
	ctx := context.Background()
	if err := a.contextManager.Restore(ctx, conversationID); err != nil {
		return err
	}

	turn, err := a.Store.LoadLatestTurn(ctx, conversationID)
	if err != nil {
		return err
	}

	if turn == nil || turn.Status != database.TurnStatusWaitingApproval {
		return nil
	}

	a.State.TurnSeq = turn.TurnSeq
	a.State.Status = turn.Status
	a.State.Iterations = turn.Iterations
	a.State.TokenUsage = turn.TokensUsed

	var pendingIDs []string
	if err := json.Unmarshal(turn.PendingTools, &pendingIDs); err != nil {
		return fmt.Errorf("decode pending tools of turn %d: %w", turn.TurnSeq, err)
	}

	a.restoreToolCalls(pendingIDs)
	return nil
}

// loadCompactedView rebuilds the LLM view from the newest compaction summary: the synthesized summary head plus
// the originals stored after its coverage anchor. A missing anchor falls back to the full history — the view
// is then un-compacted and simply re-compacts when it next crosses the threshold.
func (c *ContextManager) loadCompactedView(
	ctx context.Context,
	conversationID string,
	summary *persistent.SummaryModel,
) ([]*underlying.Message, error) {
	messages, err := c.store.LoadMessagesAfter(ctx, conversationID, summary.CoversToMessageID)
	if err == nil {
		return append([]*underlying.Message{summary.ToMessage()}, messages...), nil
	}

	if !errors.Is(err, persistent.ErrMessageNotFound) {
		return nil, err
	}

	slog.Warn("compaction anchor message missing, restoring full history",
		"conversation_id", conversationID,
		"covers_to", summary.CoversToMessageID,
	)
	return c.store.LoadMessages(ctx, conversationID)
}

// estimateTokens is a deliberately crude chars/4 heuristic over the text-bearing contents. It only
// ever measures the delta since the last provider-reported usage, or the whole view when no usage
// has been seen yet, so precision does not matter.
func estimateTokens(messages []*underlying.Message) uint64 {
	var chars uint64
	for _, m := range messages {
		for _, content := range m.Contents {
			switch c := content.(type) {
			case *underlying.TextContent:
				chars += uint64(len(c.Text))
			case *underlying.SummaryContent:
				chars += uint64(len(c.Text))
			case *underlying.ToolCallContent:
				chars += uint64(len(c.Arguments))
			case *underlying.ToolResultContent:
				chars += uint64(len(c.Result))
			case *underlying.ReasoningContent:
				chars += uint64(len(c.EncryptedContent))
			case *underlying.ToolSearchCallContent:
				chars += uint64(len(c.Arguments))
			}
		}
	}

	return chars / 4
}
