package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/internal/event"
	"github.com/taichu-lang/raven-agents/sandbox"
	"github.com/taichu-lang/raven-agents/tool"
	"github.com/taichu-lang/raven-agents/tool/functool"
)

const (
	ToolNameExec     string = "exec-command"
	ToolNameQuestion string = "question"
)

// Definition of builtin `exec_command` tool.

type ExecToolInput struct {
	Cmd   string `json:"cmd"            jsonschema:"Shell command to execute."`
	Shell string `json:"shell,omitzero" jsonschema:"Shell binary to launch. Defaults to the user's default shell."`
	TTY   bool   `json:"tty,omitzero"   jsonschema:"True allocates a PTY for the command; false or omitted uses plain pipes."`
}

// NewExecTool creates the builtin `exec-command` tool backed by the given sandbox. The tool closes over the
// sandbox only, staying stateless per call; the sandbox itself is safe for concurrent use, like the other tool
// calls in one iteration.
func NewExecTool(sb sandbox.Sandbox) tool.FuncTool {
	if sb == nil {
		panic("sandbox is required for the exec tool")
	}

	execTool, err := functool.New(functool.Config{
		Name: ToolNameExec,
		Description: "Runs a shell command in the sandbox and returns its output. " +
			"The interpreter is part of the command line itself, ex: \"python3 main.py\" or \"node script.mjs\".",
	}, func(ctx context.Context, input ExecToolInput) (string, error) {
		result, err := sb.Exec(ctx, &sandbox.ExecRequest{
			Command: input.Cmd,
			Shell:   input.Shell,
			TTY:     input.TTY,
		})
		if err != nil {
			return "", err
		}

		return formatExecResult(result), nil
	})

	if err != nil {
		panic(err)
	}

	return execTool
}

// formatExecResult renders the execution outcome as the tool result text seen by the model: stdout, then stderr
// under a marker, then status markers. A non-zero exit code is content for the model to self-correct on, not a
// tool error, so it is rendered here rather than returned as an error.
func formatExecResult(result *sandbox.ExecResult) string {
	var text strings.Builder
	text.WriteString(result.Stdout)
	if result.Stderr != "" {
		endWithNewline(&text)
		text.WriteString("[stderr]\n")
		text.WriteString(result.Stderr)
	}

	status := make([]string, 0, 3)
	if result.ExitCode != 0 {
		status = append(status, fmt.Sprintf("exit code: %d", result.ExitCode))
	}
	if result.TimedOut {
		status = append(status, "timed out")
	}
	if result.Truncated {
		status = append(status, "output truncated")
	}
	if len(status) > 0 {
		endWithNewline(&text)
		text.WriteString("[")
		text.WriteString(strings.Join(status, ", "))
		text.WriteString("]")
	}

	return text.String()
}

func endWithNewline(text *strings.Builder) {
	if text.Len() > 0 && !strings.HasSuffix(text.String(), "\n") {
		text.WriteString("\n")
	}
}

// Definition of builtin `question` tool.

type Option struct {
	Label       string `json:"label"       jsonschema:"User-facing label (1-5 words)."`
	Description string `json:"description" jsonschema:"One short sentence explaining impact/tradeoff if selected."`
}

type Question struct {
	ID       string   `json:"id"       jsonschema:"Stable identifier for mapping answers (snake_case)."`
	Header   string   `json:"header"   jsonschema:"Short header label shown in the UI (12 or fewer chars)."`
	Question string   `json:"question" jsonschema:"Single-sentence prompt shown to the user."`
	Options  []Option `json:"options"  jsonschema:"Provide 2-3 mutually exclusive choices. Put the recommended option first and suffix its label with '(Recommended)'. Do not include an 'Other' option in this list; the client will add a free-form 'Other' option automatically."`
}

type QuestionToolInput struct {
	Questions []Question `json:"questions" jsonschema:"Questions to show the user. Prefer 1 and do not exceed 3."`
}

func NewQuestionTool(bus event.Bus) tool.FuncTool {
	questionTool, err := functool.New(functool.Config{
		Name:        ToolNameQuestion,
		Description: "Use this tool when you need to ask the user questions during execution. This allows you to:\n1. Gather user preferences or requirements\n2. Clarify ambiguous instructions\n3. Get decisions on implementation choices as you work\n4. Offer choices to the user about what direction to take.\n\nUsage notes:\n- A \"Type your own answer\" option is added automatically; don't include a separate option for free form answers\n- If you recommend a specific option, make that the first option in the list and add \"(Recommended)\" at the end of the label",
	}, func(ctx context.Context, input QuestionToolInput) (string, error) {
		bus.Emit(ctx, &event.Event{
			ID:     uuid.NewString(),
			Source: event.SourceTypeAgent,
			Name:   event.EventHumanApproval,
			Payload: &event.HumanApprovalPayload{
				Scope: event.HumanApprovalScopeQuestion,
				Data:  &input,
			},
		})
		return "", nil
	})
	if err != nil {
		panic(err)
	}

	return questionTool
}

// Definition of builtin `tool_search` tool.

// toolSearchInput is the {query, limit} schema of the tool search entry. The same schema serves as the
// parameters of the native tool_search tool and, later, of the degraded function tool (design doc section 4).
type toolSearchInput struct {
	Query string `json:"query"          jsonschema:"Natural language description of the capability needed."`
	Limit int    `json:"limit,omitzero" jsonschema:"Maximum number of tools to return. Defaults to 5."`
}

// NewToolSearchTool creates the builtin tool search entry, broadcast only while the registry has deferred
// tools. On the native path the openai adapter renders this name as the {type:"tool_search",
// execution:"client"} tool and the model answers with tool_search_call items, so the handler is only a fallback
// for endpoints that surface it as a plain function call.
func NewToolSearchTool(registry *tool.ToolRegistry) tool.FuncTool {
	searchTool, err := functool.New(functool.Config{
		Name: underlying.ToolSearchToolName,
		// TODO(Leo): list the enabled MCP servers here (design doc 3.6 discoverability) once the MCP manager
		// lands; the description must stay constant within a turn.
		Description: "# Tool discovery\n\nSearches over deferred tool metadata with BM25 and exposes matching tools for the next model call.\n\nSome of the tools may not have been provided to you upfront (ex: tools provided by MCP servers), and you should use this tool (`tool_search`) to search for the required tools.",
	}, func(ctx context.Context, input toolSearchInput) (string, error) {
		hits, err := registry.Search(ctx, input.Query, input.Limit)
		if err != nil {
			return "", err
		}

		return formatToolSearchHits(hits), nil
	})
	if err != nil {
		panic(err)
	}

	return searchTool
}

// formatToolSearchHits renders the fallback function-call result as one line per hit.
func formatToolSearchHits(hits []tool.FuncTool) string {
	if len(hits) == 0 {
		return "no tools matched the query"
	}

	var sb strings.Builder
	for _, t := range hits {
		sb.WriteString(t.Name())
		if description := t.Description(); description != "" {
			sb.WriteString(": ")
			sb.WriteString(description)
		}
		sb.WriteString("\n")
	}

	return strings.TrimSuffix(sb.String(), "\n")
}

// invokeToolSearches executes the pending tool searches and checkpoints the results before any tool call runs,
// so a turn that pauses in invokeToolCalls never leaves a tool_search_call without its output (design doc 3.6).
// A failed search degrades to an empty result instead of failing the turn.
func (a *Agent) invokeToolSearches(ctx context.Context) {
	searches := a.State.ToolSearches
	a.State.ToolSearches = nil

	message := underlying.NewMessage(underlying.RoleTool, nil)
	for _, search := range searches {
		var input toolSearchInput
		if err := parseSearchArgs(search.Arguments, &input); err != nil {
			slog.Warn("failed to parse tool search arguments",
				"call_id", search.CallID, "arguments", string(search.Arguments), "err", err)
		}

		hits, err := a.ToolRegistry.Search(ctx, input.Query, input.Limit)
		if err != nil {
			slog.Warn("tool search failed", "query", input.Query, "err", err)
			hits = nil
		}

		message.Contents = append(message.Contents,
			underlying.NewToolSearchResultContent(search.CallID, toToolDefinitions(hits)))
		a.onEvent(ctx, event.SourceTypeAgent, event.EventToolSearch, &event.ToolSearchPayload{
			Query: input.Query,
			Tools: toolNames(hits),
		})
	}

	a.checkpoint(ctx, []*underlying.Message{message})
}

// parseSearchArgs decodes the {query, limit} arguments of a tool search call. Some gateways double-encode
// arguments as a JSON string (like function_call.arguments), so a leading quote is unescaped first.
func parseSearchArgs(raw json.RawMessage, input *toolSearchInput) error {
	data := []byte(raw)
	if len(data) == 0 {
		return errors.New("empty arguments")
	}

	if data[0] == '"' {
		unquoted, err := strconv.Unquote(string(data))
		if err != nil {
			return fmt.Errorf("unquote tool search arguments: %w", err)
		}

		data = []byte(unquoted)
	}

	return json.Unmarshal(data, input)
}

// toToolDefinitions maps the registry's FuncTools to message-layer definitions: the search interface does not
// depend on message types, the agent does the conversion (design doc section 4).
func toToolDefinitions(hits []tool.FuncTool) []*underlying.ToolDefinition {
	defs := make([]*underlying.ToolDefinition, 0, len(hits))
	for _, t := range hits {
		defs = append(defs, &underlying.ToolDefinition{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.InSchema(),
		})
	}

	return defs
}

func toolNames(hits []tool.FuncTool) []string {
	names := make([]string, 0, len(hits))
	for _, t := range hits {
		names = append(names, t.Name())
	}

	return names
}
