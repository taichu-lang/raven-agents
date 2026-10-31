package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/taichu-lang/raven-agents/agent"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/internal/event"
	"github.com/taichu-lang/raven-agents/internal/observability"
	"github.com/taichu-lang/raven-agents/tool"
	"github.com/taichu-lang/raven-agents/tool/functool"
)

// This example demonstrates deferred tool discovery (docs/mcp.md) without a real MCP server:
// a few hand-written FuncTools stand in for the tools an MCP server would expose, and they are
// registered with RegisterDeferred, so they never enter the tools param. Because the registry
// has deferred tools, the agent broadcasts the builtin "tool-search" entry, which the openai
// adapter renders as the native {type: "tool_search", execution: "client"} tool.
//
// The expected flow, one iteration per step:
//
//  1. the model issues a tool_search_call ("find a tool that reads kubernetes pod logs");
//  2. the agent resolves it through ToolRegistry.Search (BM25 over name, description and
//     parameter names) and checkpoints a tool_search_output carrying the full definitions;
//  3. the model calls the discovered tool by name (mcp__k8s__get_pod_logs), which the registry
//     dispatches even though the tool was never advertised;
//  4. the turn ends with the tool result rendered by the model.
//
// Note: the native path requires an endpoint that supports the tool_search tool (ex: the OpenAI
// Responses API). A gateway that silently strips unknown tool types (probed for RAVEN_API in
// docs/mcp.md section 2) leaves the model without any search entry, and the turn produces no
// tool.search event.
//
// Run with OPENAI_APIKEY / OPENAI_API set (model override: OPENAI_MODEL):
//
//	go run ./examples/02-harness/06_tool_search

// PodLogsInput mimics the input schema an MCP server would declare for its tool.
type PodLogsInput struct {
	Namespace string `json:"namespace" jsonschema:"Kubernetes namespace of the pod."`
	Pod       string `json:"pod"       jsonschema:"Name of the pod to read logs from."`
}

// PageQueryInput mimics a docs server tool that would never be discovered for a logs query.
type PageQueryInput struct {
	Query string `json:"query" jsonschema:"Full text query over the documentation pages."`
}

// newMCPServerTool builds one stand-in for a tool listed by an MCP server. The name follows the
// mcp__<server>__<tool> convention of docs/mcp.md section 3.1; in the real integration the
// manager builds these from the persisted tool snapshot and Call forwards to the server session.
func newMCPServerTool[In any](
	name, description string,
	handler func(ctx context.Context, input In) (string, error),
) tool.FuncTool {
	serverTool, err := functool.New(functool.Config{
		Name:        name,
		Description: description,
	}, handler)
	if err != nil {
		panic(err)
	}

	return serverTool
}

func main() {
	agent.UseJsonLog(agent.WithLoggerLevel("debug"))
	ctx := context.Background()
	tp, _ := observability.NewTracerProvider(ctx)
	defer tp.Shutdown(ctx)

	// The simulated MCP server tool set: only get_pod_logs matches the user's request, and the
	// search must rank it above the unrelated docs and filesystem tools.
	var podLogsCalled atomic.Bool
	k8sPodLogs := newMCPServerTool("mcp__k8s__get_pod_logs",
		"Fetches the recent logs of a kubernetes pod.",
		func(ctx context.Context, input PodLogsInput) (string, error) {
			podLogsCalled.Store(true)
			return fmt.Sprintf("[%s/%s] started serving requests", input.Namespace, input.Pod), nil
		})
	docsSearch := newMCPServerTool("mcp__docs__search_pages",
		"Searches the documentation pages.",
		func(ctx context.Context, input PageQueryInput) (string, error) {
			return "no pages matched", nil
		})
	fsRead := newMCPServerTool("mcp__fs__read_file",
		"Reads a text file from the workspace.",
		func(ctx context.Context, path string) (string, error) {
			return "file content", nil
		})

	registry := tool.NewToolRegistry(nil)
	registry.RegisterDeferred(k8sPodLogs)
	registry.RegisterDeferred(docsSearch)
	registry.RegisterDeferred(fsRead)

	bus := event.NewMemoryBus()
	subscription := bus.Subscribe()
	defer bus.Unsubscribe(subscription.ID)

	done := make(chan struct{}, 1)
	var searched atomic.Bool
	go func() {
		for e := range subscription.Channel {
			slog.Debug("on event", "source", e.Source, "name", e.Name, "payload", e.Payload)

			switch payload := e.Payload.(type) {
			case *event.ToolSearchPayload:
				searched.Store(true)
				slog.Info("tool search executed", "query", payload.Query, "tools", payload.Tools)
			case *underlying.ToolCallContent:
				slog.Info("tool call", "name", payload.Name, "arguments", payload.Arguments)
			}

			if e.Name == event.EventTurnEnd {
				done <- struct{}{}
			}
		}
	}()

	model := os.Getenv("OPENAI_MODEL")
	if model == "" {
		model = "gpt-5.5"
	}

	a := agent.New(bus, registry, &agent.AgentOptions{
		Name:           "harness-tool-search",
		UserID:         "1",
		ConversationID: uuid.NewString(),
		LLM: &underlying.ProviderOptions{
			ApiKey:   os.Getenv("OPENAI_APIKEY"),
			Endpoint: os.Getenv("OPENAI_API"),
			Instructions: "You are a helpful assistant. Tools you need may not be in your tool " +
				"list: discover them with a tool search first, then call them by name.",
			Model: model,
		},
	})

	a.Run(ctx, []*underlying.Message{
		{
			ID:   "1",
			Role: underlying.RoleUser,
			Contents: underlying.MessageContents{
				underlying.NewTextContent("Fetch the logs of pod checkout-api-7d9f in namespace prod."),
			},
		},
	})
	<-done

	a.Shutdown(ctx)

	switch {
	case !searched.Load():
		slog.Warn("no tool.search event: the endpoint did not return a tool_search_call " +
			"(native tool search unsupported or the model chose not to search)")
	case !podLogsCalled.Load():
		slog.Warn("the search ran but the discovered mcp__k8s__get_pod_logs was never called")
	default:
		slog.Info("tool search round trip succeeded: discovered and dispatched " +
			"mcp__k8s__get_pod_logs")
	}
}
