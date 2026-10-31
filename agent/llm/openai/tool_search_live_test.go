package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"testing"

	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/agent/util"
	"github.com/taichu-lang/raven-agents/tool"
)

// The live probes answer the capability question of design doc section 2: does the endpoint
// behind RAVEN_API actually return tool_search_call items for a client-executed tool_search
// tool? They are gated on RAVEN_API + RAVEN_APIKEY and run against RAVEN_MODEL (default
// qwen3.8-max).

func liveOptions(t *testing.T) *underlying.ProviderOptions {
	t.Helper()

	apiKey := os.Getenv("RAVEN_APIKEY")
	endpoint := os.Getenv("RAVEN_API")
	if apiKey == "" || endpoint == "" {
		t.Skip("RAVEN_APIKEY and RAVEN_API must be set for the live tool search probes")
	}

	model := os.Getenv("RAVEN_MODEL")
	if model == "" {
		model = "qwen3.8-max"
	}

	return &underlying.ProviderOptions{ApiKey: apiKey, Endpoint: endpoint, Model: model}
}

func newLiveProvider(options *underlying.ProviderOptions) *Provider {
	return &Provider{
		client:  util.NewStreamClient(),
		options: options,
		logger:  slog.Default().With("llm", "openai"),
	}
}

func liveToolSearchTool(t *testing.T) tool.FuncTool {
	t.Helper()
	return newTestFuncTool[toolSearchArgs](t, underlying.ToolSearchToolName,
		"Searches the catalog of deferred tools for the given query and returns the definitions "+
			"of the matching tools, which can then be called by name.")
}

// collectStream runs a streaming generation and returns the contents of the final chunks.
func collectStream(
	t *testing.T,
	p *Provider,
	messages []*underlying.Message,
	options *underlying.GenOptions,
) (underlying.MessageContents, error) {
	t.Helper()

	options.Stream = true
	var contents underlying.MessageContents
	for chunk, err := range p.Gen(context.Background(), messages, options) {
		if err != nil {
			return contents, err
		}

		if chunk != nil {
			t.Logf("chunk type=%s contents=%s", chunk.Type, dumpContents(chunk.Contents))
			if chunk.Type == underlying.ResponseChunkTypeFinal {
				contents = append(contents, chunk.Contents...)
			}
		}
	}

	return contents, nil
}

func dumpContents(contents underlying.MessageContents) string {
	encoded, err := json.Marshal(contents)
	if err != nil {
		return "<unmarshalable contents>"
	}

	return string(encoded)
}

func findToolSearchCall(contents underlying.MessageContents) *underlying.ToolSearchCallContent {
	for _, content := range contents {
		if call, ok := content.(*underlying.ToolSearchCallContent); ok {
			return call
		}
	}

	return nil
}

func searchProbeMessages() []*underlying.Message {
	return []*underlying.Message{
		underlying.NewMessage(underlying.RoleUser, underlying.MessageContents{
			underlying.NewTextContent(
				"Use tool search to find a tool that converts PDF files to markdown. " +
					"You must search the tool catalog; do not answer from memory."),
		}),
	}
}

// reportProbeVerdict fails the test with an explicit verdict for each of the three outcomes the
// probe distinguishes: gateway rejection, silent ignore, or native support confirmed.
func reportProbeVerdict(
	t *testing.T,
	contents underlying.MessageContents,
	err error,
) *underlying.ToolSearchCallContent {
	t.Helper()

	if err != nil {
		t.Fatalf("VERDICT: the gateway rejected the request; native tool_search is unavailable "+
			"on this endpoint: %v", err)
	}

	call := findToolSearchCall(contents)
	if call == nil {
		t.Fatalf("VERDICT: the stream completed with no tool_search_call item; the gateway or "+
			"the model silently ignored the tool_search tool. Final contents: %s",
			dumpContents(contents))
	}

	if call.CallID == "" {
		t.Fatalf("VERDICT: tool_search_call returned without a call id: %s",
			dumpContents(underlying.MessageContents{call}))
	}

	t.Logf("VERDICT: native tool_search confirmed; call id=%s arguments=%s",
		call.CallID, call.Arguments)
	return call
}

// TestToolSearchProbe is Variant A of the design flow: the tool_search tool is the only entry
// broadcast, and the model is prompted into searching.
func TestToolSearchProbe(t *testing.T) {
	options := liveOptions(t)
	p := newLiveProvider(options)

	contents, err := collectStream(t, p, searchProbeMessages(), &underlying.GenOptions{
		Tools: []tool.Tool{liveToolSearchTool(t)},
	})
	reportProbeVerdict(t, contents, err)
}

// TestToolSearchProbeDeferredBroadcast is Variant B, the diagnostic for a negative Variant A:
// the dummy tools ride along as function tools marked defer_loading, separating "the gateway
// drops the tool_search tool type" from "the model needs the deferred definitions present".
func TestToolSearchProbeDeferredBroadcast(t *testing.T) {
	options := liveOptions(t)
	p := newLiveProvider(options)

	params := p.newResponsesParams(options.Model, searchProbeMessages(), &underlying.GenOptions{
		Stream: true,
		Tools: []tool.Tool{
			newTestFuncTool[pdfConvertInput](t, "pdf_convert", "Converts PDF files to markdown."),
			newTestFuncTool[pdfConvertInput](t, "kubernetes_logs", "Fetches logs of kubernetes pods."),
			liveToolSearchTool(t),
		},
	})
	for i := range params.Tools {
		if function := params.Tools[i].FunctionTool; function != nil {
			function.DeferLoading = true
		}
	}

	body, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("request body: %s", body)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		options.Endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+options.ApiKey)

	streaming := p.getStreaming(req)
	defer streaming.Close()

	var contents underlying.MessageContents
	for streaming.Next() {
		_, chunk := streamEventToResponse(streaming.Current())
		if chunk == nil {
			continue
		}

		t.Logf("chunk type=%s contents=%s", chunk.Type, dumpContents(chunk.Contents))
		if chunk.Type == underlying.ResponseChunkTypeFinal {
			contents = append(contents, chunk.Contents...)
		}
	}

	if err := streaming.Err(); err != nil {
		reportProbeVerdict(t, contents, err)
	}
	reportProbeVerdict(t, contents, nil)
}

// TestToolSearchRoundTrip proves the gateway also accepts the items we echo back: after a
// confirmed search call, the history carries the tool_search_call and a tool_search_output with
// two dummy definitions, and the model must then call the discovered tool by name.
func TestToolSearchRoundTrip(t *testing.T) {
	options := liveOptions(t)
	p := newLiveProvider(options)
	searchTool := liveToolSearchTool(t)

	contents, err := collectStream(t, p, searchProbeMessages(), &underlying.GenOptions{
		Tools: []tool.Tool{searchTool},
	})
	call := reportProbeVerdict(t, contents, err)

	pdfSchema, err := tool.SchemaFor[pdfConvertInput]()
	if err != nil {
		t.Fatal(err)
	}

	history := append(searchProbeMessages(),
		underlying.NewMessage(underlying.RoleAssistant, underlying.MessageContents{call}),
		underlying.NewMessage(underlying.RoleTool, underlying.MessageContents{
			underlying.NewToolSearchResultContent(call.CallID, []*underlying.ToolDefinition{
				{
					Name:        "pdf_convert",
					Description: "Converts PDF files to markdown.",
					InputSchema: pdfSchema,
				},
				{
					Name:        "kubernetes_logs",
					Description: "Fetches logs of kubernetes pods.",
					InputSchema: pdfSchema,
				},
			}),
		}),
		underlying.NewMessage(underlying.RoleUser, underlying.MessageContents{
			underlying.NewTextContent("Now call the tool you found to convert /tmp/x.pdf."),
		}),
	)

	contents, err = collectStream(t, p, history, &underlying.GenOptions{
		Tools: []tool.Tool{searchTool},
	})
	if err != nil {
		t.Fatalf("the gateway rejected the echoed tool_search_call/tool_search_output items: %v", err)
	}

	for _, content := range contents {
		if toolCall, ok := content.(*underlying.ToolCallContent); ok {
			t.Logf("VERDICT: round trip confirmed; the model called %q with %s",
				toolCall.Name, toolCall.Arguments)
			if toolCall.Name != "pdf_convert" {
				t.Fatalf("expected the discovered pdf_convert to be called, got %q", toolCall.Name)
			}
			return
		}
	}

	t.Fatalf("the model never called the discovered tool. Final contents: %s", dumpContents(contents))
}
