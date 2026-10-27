package agent

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"slices"
	"strings"

	"github.com/taichu-lang/raven-agents/agent/hook"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/internal/event"
)

// beforeUserInput invokes hooks before user input, returns the messages that join the conversation.
//
// It resolves every skill selection the input carries: a SkillContent is a selection asking for the full skill,
// not conversation content. The message carrying it is rewritten into a Skill tool call  and its tool result,
// the same pair of messages a skill the model pulls in itself leaves behind, so both paths disclose a skill
// identically and the pair persists with the conversation. The name is folded into State.Skills, which drives
// the activation of skill-owned tools.
func (h *Agent) beforeUserInput(
	ctx context.Context,
	input []*underlying.Message,
) ([]*underlying.Message, error) {
	messages := make([]*underlying.Message, 0, len(input))
	for _, message := range input {
		for _, content := range message.Contents {
			c, ok := content.(*underlying.SkillContent)
			// By design, a selection is its own message: a message that carries a skill content carries nothing
			// else.
			if !ok {
				messages = append(messages, message)
				break
			}

			messages = append(messages, h.skillSelectionMessages(ctx, c.Name))
		}
	}

	return messages, nil
}

// skillSelectionMessages builds the message pair that stands in for a Skill tool call the model could have made
// itself: an assistant message carrying the call. Then the skill should be loaded at the beginning of the first
// iteration by tool calling.
func (h *Agent) skillSelectionMessages(
	_ context.Context,
	name string,
) *underlying.Message {
	h.recordSelectedSkill(name)
	toolID := fakeSkillToolID()
	arguments, _ := json.Marshal(skillToolInput{Name: name})

	call := underlying.NewToolCallContent(toolID, SkillToolName, string(arguments))
	// Add a tool call and let agent to load the skill using tool calling, rather than loading skill directly
	// here.
	h.State.ToolCalls = append(h.State.ToolCalls, call)
	assistant := underlying.NewMessage(underlying.RoleAssistant, underlying.MessageContents{call})
	return assistant
}

func fakeSkillToolID() string {
	b := make([]byte, 15) // length = 24
	_, _ = rand.Read(b)
	s := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
	return "toolu_" + s // length = 30
}

// recordSelectedSkill folds a successfully resolved selection into the state of this turn, once per name.
func (h *Agent) recordSelectedSkill(name string) {
	if slices.Contains(h.State.Skills, name) {
		return
	}

	h.State.Skills = append(h.State.Skills, name)
}

// beforeToolCall invokes hooks before tool calls, returns whether to continue invoking tool calls.
func (h *Agent) beforeToolCall(ctx context.Context) (bool, error) {
	result := underlying.NewMessage(underlying.RoleTool, nil)
	tools := h.State.ToolCalls
	h.State.ToolCalls = tools[:0]
	for _, call := range tools {
		if call.Name == ToolNameQuestion {
			h.State.Pending = append(h.State.Pending, call)
			continue
		}

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
			// The call is held back for the user to approve; invokeToolCalls moves the state to
			// waiting_approval once every call has been triaged.
			h.State.Pending = append(h.State.Pending, call)

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
		h.checkpoint(ctx, []*underlying.Message{result})
	}

	return continueInvokeCalls, nil
}
