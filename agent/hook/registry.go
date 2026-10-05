package hook

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Registration struct {
	Name    string
	Timeout time.Duration
	Fn      HookHandler
}

type Registry struct {
	mu    sync.RWMutex
	hooks map[Event][]Registration
}

type Result struct {
	reg Registration
	err error
	out *Output
}

func NewRegistry() *Registry {
	return &Registry{
		hooks: make(map[Event][]Registration),
	}
}

func (r *Registry) Register(event Event, reg Registration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if reg.Timeout == 0 {
		reg.Timeout = 30 * time.Second
	}

	r.hooks[event] = append(r.hooks[event], reg)
}

func (r *Registry) Run(ctx context.Context, in *Input) (*Output, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	regs := r.hooks[in.Event]
	if len(regs) == 0 {
		return &Output{
			Decision: PermissionAllow,
		}, nil
	}

	if len(regs) == 1 {
		return regs[0].Fn(ctx, in)
	}

	results := make([]Result, 0, len(regs))

	var wg sync.WaitGroup
	for i, reg := range regs {
		wg.Add(1)
		go func(i int, reg Registration) {
			defer wg.Done()
			c, cancel := context.WithTimeout(ctx, reg.Timeout)
			defer cancel()
			out, err := reg.Fn(c, in)
			results = append(results, Result{
				reg: reg,
				err: err,
				out: out,
			})
		}(i, reg)
	}
	wg.Wait()
	return makeDecisions(results), nil
}

func makeDecisions(results []Result) *Output {
	decision := &Output{}

	for _, r := range results {
		if r.err != nil || r.out == nil {
			decision.Decision = PermissionDeny
			decision.Reason = fmt.Sprintf("hook [%s] failed, detail: %v", r.reg.Name, r.err)
			return decision
		}

		if r.out.Decision == PermissionDeny {
			return r.out
		}

		if r.out.Decision == PermissionAsk {
			return r.out
		}
	}

	decision.Decision = PermissionAllow
	return decision
}
