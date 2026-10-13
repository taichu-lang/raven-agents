package openai

import (
	"encoding/json"
	"testing"

	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
)

// A selected skill is resolved by the agent into a Skill tool call and its tool result before the
// turn starts. From the api's point of view, a skill the caller selected must therefore travel
// exactly like one the model pulled in itself: a function_call item, then a function_call_output
// item joined by the call id.
func TestSelectedSkillTravelsAsFunctionCallAndOutput(t *testing.T) {
	p := &Provider{
		options: &underlying.ProviderOptions{
			Endpoint:     "https://example.invalid",
			Model:        "gpt-4.1-nano",
			Instructions: "You are a helpful assistant.",
		},
	}

	params := p.newResponsesParams("gpt-4.1-nano", []*underlying.Message{
		{
			Role: underlying.RoleUser,
			Contents: underlying.MessageContents{
				underlying.NewTextContent("fill in the pdf form"),
			},
		},
		// The pair as beforeUserInput fabricates it for a selected skill.
		{
			Role: underlying.RoleAssistant,
			Contents: underlying.MessageContents{
				underlying.NewToolCallContent("toolu_abc123", "Skill", `{"name":"pdf-processing"}`),
			},
		},
		{
			Role: underlying.RoleTool,
			Contents: underlying.MessageContents{
				underlying.NewToolResultContent("toolu_abc123", "Skill", "\"body text\"", false),
			},
		},
	}, &underlying.GenOptions{})

	if params.Instructions != "You are a helpful assistant." {
		t.Fatalf("the skill leaked into the instructions: %q", params.Instructions)
	}

	encoded, err := json.Marshal(params.Input)
	if err != nil {
		t.Fatal(err)
	}

	// Decoded rather than matched on the raw body, which the json encoder escapes.
	var decoded []struct {
		Type      string `json:"type"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
		Output    string `json:"output"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}

	if len(decoded) != 3 {
		t.Fatalf("expected the user text, the function call and its output, got %d items: %s", len(decoded), encoded)
	}

	call := decoded[1]
	if call.Type != "function_call" || call.CallID != "toolu_abc123" || call.Name != "Skill" || call.Arguments != `{"name":"pdf-processing"}` {
		t.Fatalf("unexpected function call: %+v (raw: %s)", call, encoded)
	}

	output := decoded[2]
	if output.Type != "function_call_output" || output.CallID != "toolu_abc123" || output.Output != "\"body text\"" {
		t.Fatalf("unexpected function call output: %+v (raw: %s)", output, encoded)
	}
}
