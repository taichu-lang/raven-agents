package agent

import (
	"sync"

	"github.com/taichu-lang/raven-agents/tool"
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
