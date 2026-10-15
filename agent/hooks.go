package agent

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/taichu-lang/raven-agents/agent/hook"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/agent/skill"
	"github.com/taichu-lang/raven-agents/internal/event"
)

// beforeUserInput invokes hooks before user input, returns the messages that join the conversation.
//
// It resolves every skill selection the input carries: a SkillContent is a selection asking for the
// full skill, not conversation content. The message carrying it is rewritten into a Skill tool call
// and its tool result, the same pair of messages a skill the model pulls in itself leaves behind, so
// both paths disclose a skill identically and the pair persists with the conversation. The name is
// folded into State.Skills, which drives the activation of skill-owned tools.
func (h *Agent) beforeUserInput(
	_ context.Context,
	input []*underlying.Message,
) ([]*underlying.Message, error) {
	messages := make([]*underlying.Message, 0, len(input))
	for _, message := range input {
		for _, content := range message.Contents {
			c, ok := content.(*underlying.SkillContent)
			// By design, a selection is its own message: a message that carries a skill content
			// carries nothing else.
			if !ok {
				messages = append(messages, message)
				break
			}

			loaded, err := h.SkillRegistry.Load(c.Name)
			if err != nil {
				return nil, err
			}

			h.recordSelectedSkill(loaded.Name)
			messages = skillSelectionMessages(loaded, messages)
		}
	}

	return messages, nil
}

// skillSelectionMessages builds the message pair that stands in for a Skill tool call the model
// could have made itself: an assistant message carrying the call, followed by the tool result. The
// result is encoded the way invokeToolCalls encodes every tool output, so a skill disclosed here is
// indistinguishable from one the model pulled in during the turn.
func skillSelectionMessages(loaded *skill.Loaded, messages []*underlying.Message) []*underlying.Message {
	toolID := fakeSkillToolID()
	arguments, _ := json.Marshal(skillToolInput{Name: loaded.Name})
	instructions, _ := json.Marshal(loaded.Instructions)

	messages = append(messages, &underlying.Message{
		ID:   uuid.NewString(),
		Role: underlying.RoleAssistant,
		Contents: underlying.MessageContents{
			underlying.NewToolCallContent(toolID, SkillToolName, string(arguments)),
		},
	}, &underlying.Message{
		ID:   uuid.NewString(),
		Role: underlying.RoleTool,
		Contents: underlying.MessageContents{
			underlying.NewToolResultContent(toolID, SkillToolName, string(instructions), false),
		},
	})

	return messages
}

func fakeSkillToolID() string {
	b := make([]byte, 15) // length = 24
	_, _ = rand.Read(b)
	s := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
	return "toolu_" + s // length = 30
}

// recordSelectedSkill folds a successfully resolved selection into the state of this turn, once
// per name.
func (h *Agent) recordSelectedSkill(name string) {
	if slices.Contains(h.State.Skills, name) {
		return
	}

	h.State.Skills = append(h.State.Skills, name)
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
