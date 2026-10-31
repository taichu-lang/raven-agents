package openai

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/tool"
	"github.com/taichu-lang/raven-agents/tool/functool"
)

type pdfConvertInput struct {
	Path string `json:"path" jsonschema:"PDF file path to convert."`
}

type toolSearchArgs struct {
	Query string `json:"query"          jsonschema:"Search query."`
	Limit int    `json:"limit,omitzero" jsonschema:"Maximum number of tools to return."`
}

func newTestFuncTool[In any](t *testing.T, name, description string) tool.FuncTool {
	t.Helper()

	dummy, err := functool.New(functool.Config{Name: name, Description: description},
		func(ctx context.Context, input In) (string, error) {
			return "ok", nil
		})
	if err != nil {
		t.Fatal(err)
	}

	return dummy
}

// The tool carrying the reserved name must be rendered as the native client-executed tool_search
// tool, and the plain function tools must keep every field: the shared JSON names ("type",
// "description", "parameters") would be dropped as ambiguous without Tool.MarshalJSON.
func TestToolSearchRenderedAsNativeTool(t *testing.T) {
	p := &Provider{
		options: &underlying.ProviderOptions{
			Endpoint: "https://example.invalid",
			Model:    "gpt-4.1-nano",
		},
	}

	params := p.newResponsesParams("gpt-4.1-nano", []*underlying.Message{
		underlying.NewMessage(underlying.RoleUser, underlying.MessageContents{
			underlying.NewTextContent("find a pdf tool"),
		}),
	}, &underlying.GenOptions{
		Tools: []tool.Tool{
			newTestFuncTool[pdfConvertInput](t, "pdf_convert", "Converts PDF files to markdown."),
			newTestFuncTool[pdfConvertInput](t, "kubernetes_logs", "Fetches pod logs."),
			newTestFuncTool[toolSearchArgs](t, underlying.ToolSearchToolName, "Searches deferred tools."),
		},
	})

	encoded, err := json.Marshal(params.Tools)
	if err != nil {
		t.Fatal(err)
	}

	var decoded []map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}

	if len(decoded) != 3 {
		t.Fatalf("expected 3 tools, got %d: %s", len(decoded), encoded)
	}

	if decoded[0]["type"] != "function" || decoded[0]["name"] != "pdf_convert" {
		t.Fatalf("the function tool lost fields to ambiguity: %v (raw: %s)", decoded[0], encoded)
	}

	search := decoded[2]
	if search["type"] != "tool_search" {
		t.Fatalf("the reserved name was not rendered as tool_search: %v (raw: %s)", search, encoded)
	}
	if search["execution"] != "client" {
		t.Fatalf("tool_search must be client-executed: %v", search)
	}
	if search["description"] != "Searches deferred tools." {
		t.Fatalf("unexpected tool_search description: %v", search["description"])
	}

	parameters, ok := search["parameters"].(map[string]any)
	if !ok {
		t.Fatalf("tool_search parameters missing: %v (raw: %s)", search, encoded)
	}
	properties, ok := parameters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("tool_search parameters have no properties: %v", parameters)
	}
	if _, ok := properties["query"]; !ok {
		t.Fatalf("tool_search parameters miss the query property: %v", parameters)
	}
}

// A tool search call and its result must travel back as top-level tool_search_call /
// tool_search_output items, paired by call id and carrying the full definitions of the tools
// the search resolved to.
func TestToolSearchCallAndOutputTravelAsTopLevelItems(t *testing.T) {
	p := &Provider{
		options: &underlying.ProviderOptions{
			Endpoint: "https://example.invalid",
			Model:    "gpt-4.1-nano",
		},
	}

	schema, err := tool.SchemaFor[pdfConvertInput]()
	if err != nil {
		t.Fatal(err)
	}

	params := p.newResponsesParams("gpt-4.1-nano", []*underlying.Message{
		underlying.NewMessage(underlying.RoleUser, underlying.MessageContents{
			underlying.NewTextContent("find a pdf tool"),
		}),
		underlying.NewMessage(underlying.RoleAssistant, underlying.MessageContents{
			underlying.NewToolSearchCallContent("tsc_1", "call_1",
				json.RawMessage(`{"query":"convert pdf","limit":5}`)),
		}),
		underlying.NewMessage(underlying.RoleTool, underlying.MessageContents{
			underlying.NewToolSearchResultContent("call_1", []*underlying.ToolDefinition{
				{
					Name:        "pdf_convert",
					Description: "Converts PDF files to markdown.",
					InputSchema: schema,
				},
			}),
		}),
	}, &underlying.GenOptions{})

	encoded, err := json.Marshal(params.Input)
	if err != nil {
		t.Fatal(err)
	}

	var decoded []map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}

	if len(decoded) != 3 {
		t.Fatalf("expected the user text, the search call and its output, got %d: %s",
			len(decoded), encoded)
	}

	call := decoded[1]
	if call["type"] != "tool_search_call" || call["call_id"] != "call_1" ||
		call["id"] != "tsc_1" {
		t.Fatalf("unexpected tool_search_call item: %v (raw: %s)", call, encoded)
	}
	if call["execution"] != "client" || call["status"] != "completed" {
		t.Fatalf("tool_search_call misses execution/status: %v", call)
	}
	arguments, ok := call["arguments"].(map[string]any)
	if !ok || arguments["query"] != "convert pdf" {
		t.Fatalf("tool_search_call arguments lost: %v (raw: %s)", call["arguments"], encoded)
	}

	output := decoded[2]
	if output["type"] != "tool_search_output" || output["call_id"] != "call_1" {
		t.Fatalf("unexpected tool_search_output item: %v (raw: %s)", output, encoded)
	}
	if output["execution"] != "client" || output["status"] != "completed" {
		t.Fatalf("tool_search_output misses execution/status: %v", output)
	}

	tools, ok := output["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tool_search_output carries no tools: %v (raw: %s)", output, encoded)
	}
	found, ok := tools[0].(map[string]any)
	if !ok || found["type"] != "function" || found["name"] != "pdf_convert" {
		t.Fatalf("unexpected searched tool definition: %v", tools[0])
	}
	if _, ok := found["parameters"].(map[string]any); !ok {
		t.Fatalf("searched tool definition misses parameters: %v", found)
	}
}
