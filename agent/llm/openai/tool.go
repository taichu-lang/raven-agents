package openai

import (
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/tool"
)

// FunctionTool declares the item in `tools` field of the prompt.
type FunctionTool struct {
	Type        string             `json:"type"` // Always is 'function'.
	Name        string             `json:"name"`
	Description string             `json:"description,omitzero"`
	Parameters  *jsonschema.Schema `json:"parameters,omitzero"`
	Strict      bool               `json:"strict,omitzero"`

	// DeferLoading marks a function tool as discoverable only via tool search, mirroring the defer_loading
	// field of the Responses API.
	DeferLoading bool `json:"defer_loading,omitzero"`
}

// ToolSearchTool is the client-executed tool search tool: the model issues a tool_search_call with arguments
// shaped by Parameters, and the client answers with a tool_search_output item carrying the definitions of the
// tools the search resolved to.
type ToolSearchTool struct {
	Type        string             `json:"type"` // Always is 'tool_search'.
	Execution   string             `json:"execution"`
	Description string             `json:"description,omitzero"`
	Parameters  *jsonschema.Schema `json:"parameters,omitzero"`
}

type Tool struct {
	*FunctionTool
	*ToolSearchTool
}

// MarshalJSON serializes whichever variant is set. The embedded types share JSON field names ("type",
// "description", "parameters"), so a plain struct marshal would report those fields as ambiguous and drop them;
// marshaling the set variant directly avoids that conflict.
func (t Tool) MarshalJSON() ([]byte, error) {
	switch {
	case t.FunctionTool != nil:
		return json.Marshal(t.FunctionTool)
	case t.ToolSearchTool != nil:
		return json.Marshal(t.ToolSearchTool)
	default:
		return nil, fmt.Errorf("openai: Tool has no variant set")
	}
}

type CallType string

const (
	CallTypeFunction         CallType = "function_call"
	CallTypeFunctionOutput   CallType = "function_call_output"
	CallTypeToolSearchCall   CallType = "tool_search_call"
	CallTypeToolSearchOutput CallType = "tool_search_output"
)

const (
	toolSearchExecutionClient = "client"
	toolSearchStatusCompleted = "completed"
	toolSearchTypeToolSearch  = "tool_search"
	toolSearchTypeFunction    = "function"
)

// FunctionCall is the assistant message carrying a function call. Namespace is required if the tool is found by
// 'search_tool'.
type FunctionCall struct {
	Type      CallType `json:"type,omitzero"` // "function_call"
	CallID    string   `json:"call_id,omitzero"`
	Name      string   `json:"name,omitzero"`
	Namespace string   `json:"namespace,omitzero"`
	Arguments string   `json:"arguments,omitzero"`
}

type FunctionCallOutput struct {
	Type   CallType `json:"type,omitzero"` // "function_call_output"
	CallID string   `json:"call_id,omitzero"`
	Name   string   `json:"name,omitzero"`
	Output string   `json:"output,omitzero"`
}

// ToolSearchCallItem echoes the model's tool search call back as a top-level input item on subsequent turns, so
// the api can pair it with the tool_search_output item.
type ToolSearchCallItem struct {
	Type      CallType        `json:"type,omitzero"` // "tool_search_call"
	ID        string          `json:"id,omitzero"`
	CallID    string          `json:"call_id,omitzero"`
	Execution string          `json:"execution,omitzero"` // "client"
	Status    string          `json:"status,omitzero"`    // "completed"
	Arguments json.RawMessage `json:"arguments"`
}

// ToolSearchOutputItem answers a tool_search_call with the definitions of the tools the client resolved; the
// api loads them for the rest of the conversation.
type ToolSearchOutputItem struct {
	Type      CallType        `json:"type,omitzero"` // "tool_search_output"
	CallID    string          `json:"call_id,omitzero"`
	Execution string          `json:"execution,omitzero"` // "client"
	Status    string          `json:"status,omitzero"`    // "completed"
	Tools     []*FunctionTool `json:"tools"`
}

// toolDefinitionsFromOptions builds the tool definitions to advertise to the model. Tools that do not expose a
// schema are skipped, since the Responses API requires a JSON schema of parameters for each function tool. The
// tool carrying the reserved name underlying.ToolSearchToolName is rendered as the native client-executed
// tool_search tool instead of a function tool (name as protocol).
func toolDefinitionsFromOptions(tools []tool.Tool) []Tool {
	defs := make([]Tool, 0, len(tools))
	for _, t := range tools {
		st, ok := t.(tool.SchemaTool)
		if !ok {
			continue
		}

		if st.Name() == underlying.ToolSearchToolName {
			defs = append(defs, Tool{
				ToolSearchTool: &ToolSearchTool{
					Type:        toolSearchTypeToolSearch,
					Execution:   toolSearchExecutionClient,
					Description: st.Description(),
					Parameters:  st.InSchema(),
				},
			})
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

func inputItemsFromTool(contents underlying.MessageContents) []*InputItemUnion {
	items := make([]*InputItemUnion, 0, len(contents))
	for _, mc := range contents {
		switch c := mc.(type) {
		case *underlying.ToolResultContent:
			items = append(items, &InputItemUnion{
				FunctionCallOutput: &FunctionCallOutput{
					Type:   CallTypeFunctionOutput,
					CallID: c.ToolCallID,
					Output: c.Result,
				},
			})

		case *underlying.ToolSearchResultContent:
			items = append(items, &InputItemUnion{
				ToolSearchOutputItem: &ToolSearchOutputItem{
					Type:      CallTypeToolSearchOutput,
					CallID:    c.CallID,
					Execution: toolSearchExecutionClient,
					Status:    toolSearchStatusCompleted,
					Tools:     searchedTools(c.Tools),
				},
			})

		default:
			panic("unsupported message content")
		}
	}

	return items
}

// searchedTools maps the message-layer tool definitions into the tool_search_output wire shape.
func searchedTools(defs []*underlying.ToolDefinition) []*FunctionTool {
	tools := make([]*FunctionTool, 0, len(defs))
	for _, def := range defs {
		tools = append(tools, &FunctionTool{
			Type:         toolSearchTypeFunction,
			Name:         def.Name,
			Description:  def.Description,
			Parameters:   def.InputSchema,
			Strict:       false,
			DeferLoading: true,
		})
	}

	return tools
}
