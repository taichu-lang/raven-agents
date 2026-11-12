package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/taichu-lang/raven-agents/agent/llm"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/agent/persistent"
)

const (
	// DefaultCompactSafetyFactor multiplies the catalog context window to derive the trigger threshold,
	// implicitly reserving room for the output, the tool definitions, and the growth of the current iteration.
	DefaultCompactSafetyFactor = 0.8

	// DefaultCompactKeepRecent is the number of tail messages kept verbatim through a compaction.
	DefaultCompactKeepRecent = 6

	// DefaultMaxCompactions caps the compactions of one turn.
	DefaultMaxCompactions int64 = 5

	// minDrop bounds the smallest span worth folding into a summary.
	minDrop = 2

	// toolResultTranscriptCap bounds one rendered entry inside the summarization transcript; large exec outputs
	// are the dominant token source and their head is enough for the summary.
	toolResultTranscriptCap = 2000
)

// CompactOptions configures context compaction. A nil *CompactOptions on AgentOptions selects the defaults.
type CompactOptions struct {
	// SafetyFactor multiplies Catalog.Limit.Context to derive the trigger threshold. Default 0.8.
	SafetyFactor float64

	// KeepRecent is the number of tail messages kept verbatim through a compaction. Default 6; the current
	// turn's user input always falls inside the kept tail.
	KeepRecent int

	// MaxPerTurn caps compactions within one turn; exceeding it interrupts the turn with ErrCompactionExhausted
	// rather than compacting forever. Default 5.
	MaxPerTurn int64

	// Strategy overrides the default LLM summarization strategy (truncate / prune are later strategies behind
	// this seam).
	Strategy CompactStrategy
}

func (o *CompactOptions) withDefaults() CompactOptions {
	resolved := CompactOptions{
		SafetyFactor: DefaultCompactSafetyFactor,
		KeepRecent:   DefaultCompactKeepRecent,
		MaxPerTurn:   DefaultMaxCompactions,
	}
	if o == nil {
		return resolved
	}

	if o.SafetyFactor > 0 {
		resolved.SafetyFactor = o.SafetyFactor
	}

	if o.KeepRecent > 0 {
		resolved.KeepRecent = o.KeepRecent
	}

	if o.MaxPerTurn > 0 {
		resolved.MaxPerTurn = o.MaxPerTurn
	}

	resolved.Strategy = o.Strategy
	return resolved
}

// CompactStrategy produces the replacement summary for a dropped span. prior is the previous summary text when
// compaction composes (summary-of-summary), "" otherwise.
type CompactStrategy interface {
	Summarize(
		ctx context.Context,
		prior string,
		messages []*underlying.Message,
	) (summary string, usage *underlying.UsageContent, err error)
}

// Compactor rebuilds the in-memory view when the context window is nearly full: it folds the oldest messages
// into a summary and keeps the recent tail verbatim.
type Compactor struct {
	strategy CompactStrategy
	options  CompactOptions
}

// NewCompactor builds a Compactor whose summarization calls share the agent's middleware chain
// (metering, logging, retry), plus an innermost SummaryTagMiddleware that re-tags the stream so
// the shared chain can attribute the call to compaction.
func NewCompactor(
	provider *underlying.ProviderOptions,
	options *CompactOptions,
	middlewares ...llm.Middleware,
) *Compactor {
	resolved := options.withDefaults()

	runner := llm.NewRunner(provider, append(middlewares, llm.NewSummaryTagMiddleware())...)
	strategy := resolved.Strategy
	if strategy == nil {
		strategy = &llmSummarizer{runner: runner}
	}

	return &Compactor{
		strategy: strategy,
		options:  resolved,
	}
}

// CompactResult is the outcome of one compaction.
type CompactResult struct {
	// View replaces State.Messages: [synthesized summary head] + kept tail.
	View []*underlying.Message

	// Row is the summary to persist; the store fills its conversation attributes and id.
	Row *persistent.SummaryModel

	// Dropped counts the messages folded into the summary.
	Dropped int

	// Usage is the spend of the summarization call; nil when the strategy did not report one.
	Usage *underlying.UsageContent
}

// Compact folds the prefix of messages into a summary, returning nil (and no error) when compaction cannot
// help: too little to drop, or no pair-safe boundary. When messages[0] is the synthesized head of a previous
// compaction (it carries a SummaryContent), it is folded into the dropped span while its text seeds the new
// summary (summary-of-summary).
func (c *Compactor) Compact(
	ctx context.Context,
	messages []*underlying.Message,
) (*CompactResult, error) {
	if len(messages) == 0 {
		return nil, nil
	}

	start := 0
	prior := ""
	if text, ok := summaryHeadText(messages[0]); ok {
		start = 1
		prior = text
	}

	cut, ok := chooseCutPoint(messages, start, c.options.KeepRecent)
	if !ok {
		return nil, nil
	}

	summary, usage, err := c.strategy.Summarize(ctx, prior, messages[start:cut])
	if err != nil {
		return nil, err
	}

	view := make([]*underlying.Message, 0, len(messages)-cut+1)
	view = append(view, underlying.NewMessage(underlying.RoleUser, underlying.MessageContents{
		underlying.NewSummaryContent(summary),
	}))
	view = append(view, messages[cut:]...)

	row := &persistent.SummaryModel{
		CoversFromMessageID: messages[start].ID,
		CoversToMessageID:   messages[cut-1].ID,
		Summary:             summary,
	}
	if usage != nil {
		row.Tokens = usage.Total
	}

	return &CompactResult{
		View:    view,
		Row:     row,
		Dropped: cut - start,
		Usage:   usage,
	}, nil
}

// summaryHeadText returns the summary text when message is a synthesized compaction head.
func summaryHeadText(message *underlying.Message) (string, bool) {
	for _, content := range message.Contents {
		if summary, ok := content.(*underlying.SummaryContent); ok {
			return summary.Text, true
		}
	}

	return "", false
}

// chooseCutPoint returns the index at which messages is split: [start:cut) is folded into the summary, [cut:]
// is kept verbatim. ok=false means compaction cannot help.
func chooseCutPoint(messages []*underlying.Message, start int, keepRecent int) (cut int, ok bool) {
	raw := len(messages) - keepRecent
	if raw-start < minDrop {
		return 0, false
	}

	cut = raw
	// Walk back toward start while the boundary splits a call/result pair: results always follow their call, so
	// walking back to the call's assistant message puts the whole pair into the dropped span, where the
	// summarizer narrates it.
	for cut > start && !boundarySafe(messages, cut) {
		cut--
	}

	if cut-start < minDrop {
		return 0, false
	}

	return cut, true
}

// boundarySafe reports whether splitting messages at cut keeps every tool call and its result on
// the same side. Only one direction can break: a call in the dropped span answered in the kept tail.
func boundarySafe(messages []*underlying.Message, cut int) bool {
	droppedCalls := make(map[string]struct{})
	for _, m := range messages[:cut] {
		for _, content := range m.Contents {
			switch c := content.(type) {
			case *underlying.ToolCallContent:
				droppedCalls[c.ID] = struct{}{}
			case *underlying.ToolSearchCallContent:
				droppedCalls[c.CallID] = struct{}{}
			}
		}
	}

	for _, m := range messages[cut:] {
		for _, content := range m.Contents {
			switch c := content.(type) {
			case *underlying.ToolResultContent:
				if _, split := droppedCalls[c.ToolCallID]; split {
					return false
				}
			case *underlying.ToolSearchResultContent:
				if _, split := droppedCalls[c.CallID]; split {
					return false
				}
			}
		}
	}

	return true
}

// llmSummarizer is the default CompactStrategy: it renders the dropped span as a plain-text transcript and asks
// the same model to summarize it for its future self.
type llmSummarizer struct {
	runner *llm.Runner
}

const summarizeInstruction = `You are compacting an agent conversation. Summarize the transcript below for the agent's future self: user goals, decisions made, tool actions and their outcomes, files touched, open tasks. Be terse; preserve facts, drop prose.`

func (s *llmSummarizer) Summarize(
	ctx context.Context,
	prior string,
	messages []*underlying.Message,
) (string, *underlying.UsageContent, error) {
	var prompt strings.Builder
	prompt.WriteString(summarizeInstruction)
	if prior != "" {
		prompt.WriteString("\n\nPrior summary (extend it, do not repeat it verbatim):\n")
		prompt.WriteString(prior)
	}
	prompt.WriteString("\n\nTranscript:\n")
	renderTranscript(&prompt, messages)

	return s.run(ctx, prompt.String())
}

// run consumes a non-streaming runner call and returns the final text with its usage. The SummaryTagMiddleware
// re-tags the final chunk as summary, so both final and summary are accepted.
func (s *llmSummarizer) run(ctx context.Context, prompt string) (string, *underlying.UsageContent, error) {
	var text string
	var usage *underlying.UsageContent
	for chunk, err := range s.runner.Run(ctx, []*underlying.Message{
		underlying.NewMessage(underlying.RoleUser, underlying.MessageContents{
			underlying.NewTextContent(prompt),
		}),
	}, underlying.WithStream(false)) {
		if err != nil {
			return "", nil, err
		}

		switch chunk.Type {
		case underlying.ResponseChunkTypeFinal, underlying.ResponseChunkTypeSummary:
			text = chunk.Contents.CollectText()
		case underlying.ResponseChunkTypeUsage:
			if len(chunk.Contents) > 0 {
				usage, _ = chunk.Contents[0].(*underlying.UsageContent)
			}
		}
	}

	if text == "" {
		return "", nil, errors.New("summarization returned empty text")
	}

	return text, usage, nil
}

// renderTranscript renders one line per content item; compact enough for the summarizer to read, complete
// enough to reconstruct what happened.
func renderTranscript(sb *strings.Builder, messages []*underlying.Message) {
	for _, m := range messages {
		for _, content := range m.Contents {
			switch c := content.(type) {
			case *underlying.TextContent:
				fmt.Fprintf(sb, "%s: %s\n", m.Role, c.Text)
			case *underlying.SummaryContent:
				// The previous summary enters through the prior parameter, never the transcript.
			case *underlying.ReasoningContent:
				// Reasoning is model-internal; the summary narrates outcomes, not thoughts.
			case *underlying.ToolCallContent:
				fmt.Fprintf(sb, "assistant called tool %s(%s)\n", c.Name, truncate(c.Arguments, toolResultTranscriptCap))
			case *underlying.ToolSearchCallContent:
				fmt.Fprintf(sb, "assistant searched tools %s\n", truncate(string(c.Arguments), toolResultTranscriptCap))
			case *underlying.ToolResultContent:
				fmt.Fprintf(sb, "tool result %s (is_error=%v): %s\n", c.Name, c.IsError, truncate(c.Result, toolResultTranscriptCap))
			case *underlying.ToolSearchResultContent:
				names := make([]string, 0, len(c.Tools))
				for _, t := range c.Tools {
					names = append(names, t.Name)
				}
				fmt.Fprintf(sb, "tool search resolved: %s\n", strings.Join(names, ", "))
			}
		}
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}

	return s[:max] + "...(truncated)"
}
