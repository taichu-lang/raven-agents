package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/agent/persistent"
)

func (a *Agent) restoreIfPresent(conversationID string) error {
	ctx := context.Background()
	messages, err := a.Store.LoadMessages(ctx, conversationID)
	if err != nil {
		return err
	}

	if len(messages) > 0 {
		a.State.Messages = append(a.State.Messages, messages...)
	}

	turn, err := a.Store.LoadLatestTurn(ctx, conversationID)
	if err != nil {
		return err
	}

	if turn == nil || turn.Status != persistent.TurnStatusWaitingApproval {
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

// restoreToolCalls rebuilds Pending and ToolCalls from the last assistant message. Pending is what the hooks
// held back, which is the only part that cannot be derived; ToolCalls are the calls the hooks let through that
// still have no result.
func (a *Agent) restoreToolCalls(pendingIDs []string) {
	answered := make(map[string]struct{})
	for i := len(a.State.Messages) - 1; i >= 0; i-- {
		msg := a.State.Messages[i]
		if msg.Role == underlying.RoleTool {
			for _, content := range msg.Contents {
				if r, ok := content.(*underlying.ToolResultContent); ok {
					answered[r.ToolCallID] = struct{}{}
				}
			}

			continue
		}

		if msg.Role != underlying.RoleAssistant {
			return
		}

		for _, content := range msg.Contents {
			call, ok := content.(*underlying.ToolCallContent)
			if !ok {
				continue
			}

			if _, done := answered[call.ID]; done {
				continue
			}

			if slices.Contains(pendingIDs, call.ID) {
				a.State.Pending = append(a.State.Pending, call)
			} else {
				a.State.ToolCalls = append(a.State.ToolCalls, call)
			}
		}

		return
	}
}

// saveTurn syncs the mutable turn state, including the calls waiting for the user, to the store.
func (a *Agent) saveTurn(ctx context.Context) {
	pendingIDs := make([]string, 0, len(a.State.Pending))
	for _, call := range a.State.Pending {
		pendingIDs = append(pendingIDs, call.ID)
	}

	a.State.PendingTools = pendingIDs
	_ = a.Store.UpdateTurn(ctx, &a.State.TurnContext)
}
