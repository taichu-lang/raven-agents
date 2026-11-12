package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/taichu-lang/raven-agents/agent"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/pkg/database"
	"github.com/taichu-lang/raven-agents/pkg/event"
	"github.com/taichu-lang/raven-agents/pkg/observability"
	"github.com/taichu-lang/raven-agents/tool"
)

// This example demonstrates context compaction: a low Budget.MaxContextTokens forces compaction
// after a couple of turns, the conversation keeps working from the summary, and a fresh agent on
// the same store rebuilds the compacted view from the message_summaries table.
func main() {
	agent.UseJsonLog(agent.WithLoggerLevel("debug"))
	ctx := context.Background()
	tp, _ := observability.NewTracerProvider(ctx)
	defer tp.Shutdown(ctx)

	bus := event.NewMemoryBus()
	subscription := bus.Subscribe()
	defer bus.Unsubscribe(subscription.ID)

	var compactions atomic.Int64
	go func() {
		for e := range subscription.Channel {
			switch e.Name {
			case event.EventCompactStart, event.EventCompactEnd:
				slog.Info("compaction event", "name", e.Name, "payload", e.Payload)
				if e.Name == event.EventCompactEnd {
					compactions.Add(1)
				}
			case event.EventLLMComplete:
				slog.Info("assistant message is accepted", "message", e.Payload)
			}
		}
	}()

	storePath := filepath.Join(os.TempDir(), "raven_context_compact_demo.db")
	_ = os.Remove(storePath)

	conversationID := uuid.NewString()
	a := newAgent(bus, storePath, conversationID)

	runTurn(ctx, a, "Hi, i am leo, my favorite number is 42.")

	// Keep the conversation going until at least one compaction has completed; the projected
	// context crosses the 800-token budget a turn or two into the padding.
	padding := []string{
		"Tell me a long story about a cat who learns to swim.",
		"Now tell me another long story, this time about a dog who flies a plane.",
		"One more story: a penguin who opens a bakery.",
		"Tell me about a robot who learns to paint.",
		"Tell me about a whale who sings opera.",
		"Tell me about a squirrel who trades stocks.",
	}
	for i, question := range padding {
		runTurn(ctx, a, question)
		if compactions.Load() > 0 {
			break
		}
		if i == len(padding)-1 {
			slog.Warn("no compaction fired; the demo still answers, but from full history")
		}
	}

	// The first-turn facts only exist in the summary by now: this turn proves the agent can
	// still answer from the compacted view.
	runTurn(ctx, a, "What is my name and my favorite number?")

	if err := a.Shutdown(ctx); err != nil {
		slog.Warn("failed to shutdown the first agent", "err", err)
	}

	// A fresh agent on the same store rebuilds the compacted view from the message_summaries
	// table: the answer must come from the restored summary, not the originals.
	restarted := newAgent(bus, storePath, conversationID)
	runTurn(ctx, restarted, "Once more: who am i?")

	if err := restarted.Shutdown(ctx); err != nil {
		slog.Warn("failed to shutdown the restarted agent", "err", err)
	}
}

func newAgent(bus event.Bus, storePath string, conversationID string) *agent.Agent {
	return agent.New(bus, tool.NewToolRegistry(nil), &agent.AgentOptions{
		Name:           "context-compact",
		UserID:         "1",
		ConversationID: conversationID,
		LLM: &underlying.ProviderOptions{
			ApiKey:       os.Getenv("RAVEN_APIKEY"),
			TextURL:      os.Getenv("RAVEN_API"),
			Instructions: "You are a helpful assistant!",
			Catalog: &underlying.Catalog{
				ID: "qwen3.8-flash",
				Limit: underlying.Limit{
					Context: 800,
				},
			},
		},
		Compact: &agent.CompactOptions{
			KeepRecent: 2,
		},
		StoreCfg: &database.StoreConfig{
			Sqlite: &database.SqliteConfig{
				Path: storePath,
			},
		},
	})
}

func runTurn(ctx context.Context, a *agent.Agent, input string) {
	slog.Info("user turn", "input", input)
	if err := a.Run(ctx, []*underlying.Message{
		{
			ID:   uuid.NewString(),
			Role: underlying.RoleUser,
			Contents: underlying.MessageContents{
				underlying.NewTextContent(input),
			},
		},
	}); err != nil {
		panic(err)
	}
}
