package tool

import (
	"context"
	"sync"
)

// DefaultToolSearchLimit is the number of tools a search returns when the caller does not specify one.
const DefaultToolSearchLimit = 5

type ToolActiveFunc func() bool

type ToolRegistration struct {
	FuncTool
	Active ToolActiveFunc
}

// ToolRegistry holds the tools of one agent. Broadcast tools are registered via Register and enter the tools
// param through Active; deferred tools are registered via RegisterDeferred and are dispatch-only: the model
// discovers them through the tool search result message, never through the tools param. The two sets are
// guarded by separate mutexes so that an active func may call HasDeferred while Active holds the broadcast
// lock.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]ToolRegistration

	deferredMu sync.RWMutex
	deferred   map[string]FuncTool

	searcher ToolSearcher
}

// NewToolRegistry creates a registry; a nil searcher falls back to NewBM25Searcher.
func NewToolRegistry(searcher ToolSearcher) *ToolRegistry {
	if searcher == nil {
		searcher = NewBM25Searcher()
	}

	return &ToolRegistry{
		tools:    make(map[string]ToolRegistration),
		deferred: make(map[string]FuncTool),
		searcher: searcher,
	}
}

func (r *ToolRegistry) Register(t FuncTool, active ToolActiveFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.tools[t.Name()] = ToolRegistration{
		FuncTool: t,
		Active:   active,
	}
}

// RegisterDeferred registers a dispatch-only tool: it is never advertised in the tools param;
// it enters the model's view only via the search result message. Get covers it.
// TODO(Leo): add mcp name.
func (r *ToolRegistry) RegisterDeferred(t FuncTool) {
	r.deferredMu.Lock()
	defer r.deferredMu.Unlock()

	r.deferred[t.Name()] = t
}

// HasDeferred reports whether any dispatch-only tool is registered, which decides whether the tool search entry
// is advertised.
func (r *ToolRegistry) HasDeferred() bool {
	r.deferredMu.RLock()
	defer r.deferredMu.RUnlock()

	return len(r.deferred) > 0
}

// Search resolves deferred tools for the query. It snapshots the deferred set under the read lock, calls the
// searcher without holding it, and drops hits outside the deferred set, so "searchable" and "dispatchable" are
// always the same set.
func (r *ToolRegistry) Search(ctx context.Context, query string, limit int) ([]FuncTool, error) {
	if limit <= 0 {
		limit = DefaultToolSearchLimit
	}

	r.deferredMu.RLock()
	candidates := make([]FuncTool, 0, len(r.deferred))
	for _, t := range r.deferred {
		candidates = append(candidates, t)
	}
	r.deferredMu.RUnlock()

	hits, err := r.searcher.Search(ctx, query, limit, candidates)
	if err != nil {
		return nil, err
	}

	r.deferredMu.RLock()
	defer r.deferredMu.RUnlock()

	// Sync hits with the deferred set again, in case the set has changed (ex: mcp server changed).
	filtered := make([]FuncTool, 0, len(hits))
	for _, hit := range hits {
		if t, ok := r.deferred[hit.Name()]; ok {
			filtered = append(filtered, t)
		}
	}

	return filtered, nil
}

func (r *ToolRegistry) Active() []FuncTool {
	tools := make([]FuncTool, 0, 1)
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, tool := range r.tools {
		if tool.Active == nil || tool.Active() {
			tools = append(tools, tool.FuncTool)
		}
	}

	return tools
}

// Get looks a tool up by name in the broadcast set first, then in the deferred set, so a tool discovered
// mid-turn is dispatch-able by name.
func (r *ToolRegistry) Get(name string) FuncTool {
	r.mu.RLock()
	registered, ok := r.tools[name]
	r.mu.RUnlock()
	if ok {
		return registered.FuncTool
	}

	r.deferredMu.RLock()
	defer r.deferredMu.RUnlock()

	return r.deferred[name]
}
