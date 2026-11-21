package agent

import (
	"embed"
	"fmt"

	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
)

//go:embed prompts
var promptDir embed.FS

var prompts map[string]string

func init() {
	prompts = make(map[string]string)
	keys := []string{
		ToolNameSkill,
		ToolNameExec,
		ToolNameQuestion,
		ToolNameWebFetch,
		ToolNameWebSearch,
		underlying.ToolSearchToolName,
	}
	for _, key := range keys {
		md, err := promptDir.ReadFile(fmt.Sprintf("prompts/%s.md", key))
		if err != nil {
			panic(err)
		}

		prompts[key] = string(md)
	}
}
