package openai

import (
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/taichu-lang/raven-agents/agent/llm"
	"github.com/taichu-lang/raven-agents/tool"
)

type FunctionTool struct {
	Type        string             `json:"type"` // Always is 'function'.
	Name        string             `json:"name"`
	Description string             `json:"description,omitzero"`
	Parameters  *jsonschema.Schema `json:"parameters,omitzero"`
	Strict      bool               `json:"strict,omitzero"`
}

type Tool struct {
	*FunctionTool
}

type CallType string

const (
	CallTypeFunction       CallType = "function_call"
	CallTypeFunctionOutput CallType = "function_call_output"
)

type FunctionCall struct {
	Type      CallType `json:"type,omitzero"` // "function_call"
	CallID    string   `json:"call_id,omitzero"`
	Name      string   `json:"name,omitzero"`
	Arguments string   `json:"arguments,omitzero"`
}

type FunctionCallOutput struct {
	Type   CallType `json:"type,omitzero"` // "function_call_output"
	CallID string   `json:"call_id,omitzero"`
	Name   string   `json:"name,omitzero"`
	Output string   `json:"output,omitzero"`
}

// toolDefinitionsFromOptions builds the tool definitions to advertise to the model.
// Tools that do not expose a schema are skipped, since the Responses API requires
// a JSON schema of parameters for each function tool.
func toolDefinitionsFromOptions(tools []tool.Tool) []Tool {
	defs := make([]Tool, 0, len(tools))
	for _, t := range tools {
		st, ok := t.(tool.SchemaTool)
		if !ok {
			continue
		}

		defs = append(defs, Tool{
			FunctionTool: &FunctionTool{
				Type:        "function",
				Name:        st.Name(),
				Description: st.Description(),
				Parameters:  st.InSchema(),
			},
		})
	}

	return defs
}

func inputItemsFromTool(contents llm.MessageContents) []*InputItemUnion {
	items := make([]*InputItemUnion, 0, len(contents))
	for _, mc := range contents {
		result, ok := mc.(*llm.ToolResultContent)
		if !ok {
			panic("unsupported message content")
		}

		items = append(items, &InputItemUnion{
			FunctionCallOutput: &FunctionCallOutput{
				Type:   CallTypeFunctionOutput,
				CallID: result.ToolCallID,
				Output: result.Result,
			},
		})
	}

	return items
}
