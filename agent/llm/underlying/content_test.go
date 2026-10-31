package underlying

import (
	"encoding/json"
	"testing"

	"github.com/taichu-lang/raven-agents/tool"
)

type roundTripInput struct {
	Path string `json:"path" jsonschema:"File path."`
}

// The tool search contents are persisted with the conversation; a kind missing from
// unmarshalMessageContent would break restoreIfPresent and panic agent.New for every session
// that contains a search.
func TestToolSearchContentsRoundTrip(t *testing.T) {
	schema, err := tool.SchemaFor[roundTripInput]()
	if err != nil {
		t.Fatal(err)
	}

	contents := MessageContents{
		NewToolSearchCallContent("tsc_1", "call_1", json.RawMessage(`{"query":"convert pdf","limit":5}`)),
		NewToolSearchResultContent("call_1", []*ToolDefinition{
			{
				Name:        "pdf_convert",
				Description: "Converts PDF files to markdown.",
				InputSchema: schema,
			},
		}),
	}

	data, err := json.Marshal(contents)
	if err != nil {
		t.Fatal(err)
	}

	restored, err := UnmarshalMessageContents(data)
	if err != nil {
		t.Fatal(err)
	}

	if len(restored) != 2 {
		t.Fatalf("expected 2 restored contents, got %d: %s", len(restored), data)
	}

	call, ok := restored[0].(*ToolSearchCallContent)
	if !ok {
		t.Fatalf("the search call restored as %T", restored[0])
	}
	if call.ID != "tsc_1" || call.CallID != "call_1" {
		t.Fatalf("unexpected search call: %+v", call)
	}
	if string(call.Arguments) != `{"query":"convert pdf","limit":5}` {
		t.Fatalf("search call arguments not kept raw: %s", call.Arguments)
	}

	result, ok := restored[1].(*ToolSearchResultContent)
	if !ok {
		t.Fatalf("the search result restored as %T", restored[1])
	}
	if result.CallID != "call_1" || len(result.Tools) != 1 {
		t.Fatalf("unexpected search result: %+v", result)
	}
	definition := result.Tools[0]
	if definition.Name != "pdf_convert" || definition.Description != "Converts PDF files to markdown." {
		t.Fatalf("unexpected tool definition: %+v", definition)
	}
	if definition.InputSchema == nil {
		t.Fatal("the tool definition lost its input schema")
	}
	if _, ok := definition.InputSchema.Properties["path"]; !ok {
		t.Fatalf("the input schema lost its properties: %+v", definition.InputSchema)
	}
}
