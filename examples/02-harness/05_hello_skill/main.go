package main

import (
	"context"
	"log/slog"
	"os"
	"path"

	"github.com/taichu-lang/raven-agents/agent"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/internal/event"
	"github.com/taichu-lang/raven-agents/internal/observability"
)

// Install a hello-world skill in $HOME/.agents/skills/hello-world directory, its SKILL.md content is:
//
// ---
// name: hello-world
// description: A minimal skill example - use when learning the skill format
// ---
//
// # Hello World Skill
//
// This is the simplest possible skill. It demonstrates the required format.
//
// ## When This Skill Activates
//
// This skill loads when you mention "hello world skill" or "test my skills setup".
//
// ## What It Does
//
// Responds with a friendly confirmation that skills are working.
//
// ## Try It
//
// Ask Copilot: "Test my skills setup" or "Use the hello world skill"
func main() {
	agent.UseJsonLog(agent.WithLoggerLevel("debug"))
	ctx := context.Background()
	tp, _ := observability.NewTracerProvider(ctx)
	defer tp.Shutdown(ctx)

	bus := event.NewMemoryBus()

	subscription := bus.Subscribe()
	defer bus.Unsubscribe(subscription.ID)

	done := make(chan struct{}, 1)

	go func() {
		for e := range subscription.Channel {
			slog.Debug("on event", "source", e.Source, "name", e.Name, "payload", e.Payload)
			if e.Name == event.EventLLMComplete {
				done <- struct{}{}
			}
		}
	}()

	home, _ := os.UserHomeDir()

	a := agent.New(bus, agent.NewToolRegistry(), &agent.AgentOptions{
		Name: "hello-skill",
		LLM: &underlying.ProviderOptions{
			ApiKey:       os.Getenv("OPENAI_APIKEY"),
			Endpoint:     os.Getenv("OPENAI_API"),
			Instructions: "You are a helpful assistant!",
			Model:        "gpt-4.1-nano",
		},
		SkillDirs: []string{path.Join(home, ".agents", "skills")},
	})
	a.Run(ctx, []*underlying.Message{
		{
			ID:   "1",
			Role: underlying.RoleUser,
			Contents: underlying.MessageContents{
				underlying.NewTextContent("hello world skill"),
			},
		},
		{
			ID:   "2",
			Role: underlying.RoleUser,
			Contents: underlying.MessageContents{
				underlying.NewUnresolvedSkillContent("hello-world"),
			},
		},
	})
	<-done

	a.Shutdown(ctx)
}
