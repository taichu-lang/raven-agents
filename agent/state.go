package agent

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/agent/persistent"
	"github.com/taichu-lang/raven-agents/pkg/database"
	"github.com/taichu-lang/raven-agents/pkg/webcrawler"
)

// AgentState represents the shared state of the agent, which can be accessed and modified by all nodes in the
// graph. Fields in this struct are intended to be persistent, and can be resumed after a restart of the agent.
type AgentState struct {
	// ToolCalls is the list of tools returned from llm, and to be called from agent.
	ToolCalls []*underlying.ToolCallContent

	// ToolSearches is the list of tool searches returned from llm. They are executed before the tool calls of
	// the same iteration and their results are check-pointed right away, so they never enter the persisted turn
	// state: a paused turn has no unanswered search.
	ToolSearches []*underlying.ToolSearchCallContent

	// Pending is the list of actions waiting for human approval.
	Pending []*underlying.ToolCallContent

	// Skills holds the names of the skills the caller selected for this turn. beforeUserInput turns each
	// selection into a Skill tool call and its tool result, so the instructions live in Messages like any other
	// conversation content; only the names are kept here, as the state must stay light and the names drive the
	// activation of skill-owned tools.
	Skills []string

	// Activated holds the names of the skills the model pulled in itself, through the skill tool. Their
	// instructions already live in Messages as a tool result, so these names are tracked for the activation of
	// skill-owned tools, not to inject anything.
	Activated []string

	// Compactions records the number of compactions performed in this turn.
	Compactions int64
	persistent.TurnContext
	Annotations *AnnotationSourceRegistry
}

func (s *AgentState) resetPerTurn(turn *database.TurnModel) {
	// Both selections last for one turn: the caller restates its own with every Run, and what the model pulled
	// in has already reached it as a tool result.
	s.Skills = s.Skills[:0]
	s.Activated = s.Activated[:0]
	s.ToolCalls = s.ToolCalls[:0]
	s.ToolSearches = s.ToolSearches[:0]
	s.Pending = s.Pending[:0]
	s.Iterations = 0
	s.TokenUsage = 0
	s.TokenCost = 0
	s.StartedAt = time.Now()
	s.Compactions = 0
	s.TurnSeq = turn.TurnSeq
	if s.Annotations != nil {
		s.Annotations.turnSeq = turn.TurnSeq
	}
}

// activateSkill records a skill the model pulled in, ignoring one that is already recorded.
func (s *AgentState) activateSkill(name string) {
	if slices.Contains(s.Activated, name) {
		return
	}

	s.Activated = append(s.Activated, name)
}

func (s *AgentState) onFinalChunk(chunk *underlying.ResponseChunk) *underlying.Message {
	assistant := &underlying.Message{
		ID:       chunk.ID,
		Role:     chunk.Role,
		Contents: chunk.Contents,
	}

	for _, content := range chunk.Contents {
		if call, ok := content.(*underlying.ToolCallContent); ok {
			s.ToolCalls = append(s.ToolCalls, call)
		}

		if search, ok := content.(*underlying.ToolSearchCallContent); ok {
			s.ToolSearches = append(s.ToolSearches, search)
		}
	}

	return assistant
}

func (s *AgentState) newAnnotationSourceRegistry() *AnnotationSourceRegistry {
	s.Annotations = &AnnotationSourceRegistry{
		sources:   make(map[string]*WebSource),
		evidences: make([]*WebSource, 0),
		turnSeq:   s.TurnSeq,
	}

	return s.Annotations
}

const (
	DefaultMaxIterations int64 = 500
)

// Budget represents the resource limits for the agent, which can be used to control the execution of the agent
// and prevent it from consuming too many resources.
type Budget struct {
	// MaxIterations is the maximum number of loop iterations in each turn, to prevent infinite loops.
	MaxIterations int64

	// MaxDuration is the maximum duration of the agent execution, in seconds, to prevent long-running.
	MaxDuration int64

	// MaxTotalTokens is the maximum number of tokens in the entire execution.
	MaxTotalTokens int64

	// MaxCost is the maximum cost of the agent execution, in dollars, to prevent overspending.
	MaxCost float64
}

func (b Budget) Exceeded(s *AgentState) bool {
	if b.MaxIterations > 0 && s.Iterations >= b.MaxIterations {
		return true
	}

	if b.MaxTotalTokens > 0 && s.TokenUsage >= b.MaxTotalTokens {
		return true
	}

	if b.MaxCost > 0 && s.TokenCost >= b.MaxCost {
		return true
	}

	if b.MaxDuration > 0 && time.Since(s.StartedAt).Seconds() >= float64(b.MaxDuration) {
		return true
	}

	return false
}

// WebSource holds the basic information of a web page. It is query-independent: the same URL always maps to the
// same page, and therefore the same ID, no matter whether it was discovered by web_search or by web_fetch.
// The ID is handed to the LLM as the source ID for citation.
type WebSource struct {
	ID      string `json:"id"`
	URL     string `json:"url"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

// AnnotationSourceRegistry is the turn-scoped registry of external web pages. Currently, it only supports
// web_search and web_fetch.
type AnnotationSourceRegistry struct {
	sync.RWMutex
	turnSeq   int64
	sources   map[string]*WebSource // key is `url`.
	evidences []*WebSource          // sources in registration order.
}

// marshalEvidences encodes the sources registered so far as a JSON array.
func (asr *AnnotationSourceRegistry) marshalEvidences() json.RawMessage {
	asr.RLock()
	defer asr.RUnlock()

	if raw, err := json.Marshal(asr.evidences); err != nil {
		return nil
	} else {
		return raw
	}
}

func (asr *AnnotationSourceRegistry) addSearchResult(r *webcrawler.SearchResult) *WebSource {
	asr.Lock()
	defer asr.Unlock()

	if source, ok := asr.sources[r.URL]; ok {
		if source.Title == "" {
			source.Title = r.Title
		}

		return source
	}

	page := &WebSource{
		ID:    fmt.Sprintf("turn-%d-%d", asr.turnSeq, len(asr.evidences)),
		URL:   r.URL,
		Title: r.Title,
	}

	if r.Snippet != "" {
		page.Content = r.Snippet
	} else {
		page.Content = r.Summary
	}

	asr.sources[r.URL] = page
	asr.evidences = append(asr.evidences, page)

	return page
}

func (asr *AnnotationSourceRegistry) addFetchResult(r *webcrawler.FetchResult) *WebSource {
	asr.Lock()
	defer asr.Unlock()

	content := substring(r.RawContent, 200)
	if source, ok := asr.sources[r.URL]; ok {
		if source.Content == "" {
			source.Content = content
		}

		return source
	}

	page := &WebSource{
		ID:      fmt.Sprintf("turn-%d-%d", asr.turnSeq, len(asr.evidences)),
		URL:     r.URL,
		Content: content,
	}
	asr.sources[r.URL] = page
	asr.evidences = append(asr.evidences, page)

	return page
}

func substring(s string, length int) string {
	runes := []rune(s)

	if len(runes) > length {
		return string(runes[:length])
	}

	return s
}
