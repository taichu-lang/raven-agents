package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/taichu-lang/raven-agents/internal/event"
	"github.com/taichu-lang/raven-agents/sandbox"
	"github.com/taichu-lang/raven-agents/tool"
	"github.com/taichu-lang/raven-agents/tool/functool"
)

const (
	ToolNameExec     string = "exec-command"
	ToolNameQuestion string = "question"
)

type ToolActiveFunc func(state *AgentState) bool

type ToolRegistration struct {
	tool.FuncTool
	Active ToolActiveFunc
}

type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]ToolRegistration
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]ToolRegistration),
	}
}

func (r *ToolRegistry) Register(t tool.FuncTool, active ToolActiveFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.tools[t.Name()] = ToolRegistration{
		FuncTool: t,
		Active:   active,
	}
}

func (r *ToolRegistry) Active(state *AgentState) []tool.FuncTool {
	tools := make([]tool.FuncTool, 0, 1)
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, tool := range r.tools {
		if tool.Active == nil || tool.Active(state) {
			tools = append(tools, tool.FuncTool)
		}
	}

	return tools
}

func (r *ToolRegistry) Get(name string) tool.FuncTool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if tool, ok := r.tools[name]; ok {
		return tool.FuncTool
	}

	return nil
}

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
