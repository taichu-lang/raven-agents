package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/taichu-lang/raven-agents/agent/skill"
	"github.com/taichu-lang/raven-agents/tool"
	"github.com/taichu-lang/raven-agents/tool/functool"
)

// SkillToolName is the name of the builtin tool the model calls to pull in the instructions of a
// skill it has only seen named in the catalog.
const SkillToolName = "Skill"

var ErrSkillNotFound = errors.New("skill not found")

type SkillActiveFunc func(state *AgentState) bool

type SkillRegistration struct {
	skill.Skill
	Active SkillActiveFunc
}

// SkillRegistry holds the skills installed in the system. It is the only place a skill name is
// turned into an installation directory, so an installation path that reached the process from the
// outside, ex: carried by an api request, is never read from disk.
//
// A registry is safe for concurrent use and is meant to be built once per process and shared.
type SkillRegistry struct {
	mu     sync.RWMutex
	loader *skill.Loader
	skills map[string]SkillRegistration
}

func NewSkillRegistry(dirs []string) *SkillRegistry {
	loader := skill.NewLoader(skill.DefaultMaxSize)

	registry := &SkillRegistry{
		loader: loader,
		skills: make(map[string]SkillRegistration),
	}
	for _, dir := range dirs {
		installed, err := loader.Scan(dir)
		if err != nil {
			slog.Error("failed to load skill", "dir", dir, "err", err)
			continue
		}

		for _, found := range installed {
			registry.Register(found, nil)
		}
	}

	return registry
}

// Register installs s under its own name. A nil active means the skill is always offered.
func (r *SkillRegistry) Register(s skill.Skill, active SkillActiveFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.skills[s.Name] = SkillRegistration{
		Skill:  s,
		Active: active,
	}
}

func (r *SkillRegistry) Get(name string) (skill.Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	registration, ok := r.skills[name]
	if !ok {
		return skill.Skill{}, false
	}

	return registration.Skill, true
}

// Names returns every registered skill name, sorted.
func (r *SkillRegistry) Names() []string {
	r.mu.RLock()
	names := make([]string, 0, len(r.skills))
	for name := range r.skills {
		names = append(names, name)
	}
	r.mu.RUnlock()

	slices.Sort(names)

	return names
}

// Available returns every registered skill that is active for the given state, sorted by name so
// that an unchanged set of skills yields the same order on every call.
func (r *SkillRegistry) Available(state *AgentState) []skill.Skill {
	r.mu.RLock()
	available := make([]skill.Skill, 0, len(r.skills))
	for _, registration := range r.skills {
		if registration.Active != nil && !registration.Active(state) {
			continue
		}

		available = append(available, registration.Skill)
	}
	r.mu.RUnlock()

	slices.SortFunc(available, func(a, b skill.Skill) int {
		return strings.Compare(a.Name, b.Name)
	})

	return available
}

// Load resolves name through the registry and reads the manifest of the skill it names.
func (r *SkillRegistry) Load(name string) (*skill.Loaded, error) {
	s, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrSkillNotFound, name)
	}

	return r.loader.Load(s)
}

// Resolve turns a caller supplied selection into the registered skills, failing on the first name
// that is not installed. Callers validate a selection with it before a turn starts, so that a bad
// name is reported as a plain error instead of surfacing halfway through a stream.
func (r *SkillRegistry) Resolve(names []string) ([]skill.Skill, error) {
	resolved := make([]skill.Skill, 0, len(names))
	for _, name := range names {
		s, ok := r.Get(name)
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrSkillNotFound, name)
		}

		resolved = append(resolved, s)
	}

	return resolved, nil
}

type skillToolInput struct {
	Name string `json:"name" jsonschema:"the name of the skill to load, exactly as it is listed in the available skills"`
}

// NewSkillTool builds the builtin skill tool.
//
// The tool closes over the registry alone, and deliberately not over the agent or its state: tool
// calls of one iteration are invoked concurrently, and a registry is shared between agents. Which
// skills the model has pulled in is recorded by the agent itself, once the calls have been joined.
func NewSkillTool(registry *SkillRegistry) tool.FuncTool {
	tool, err := functool.New(functool.Config{
		Name: SkillToolName,
		Description: "Load the full instructions of one of the available skills. Call it with the " +
			"name of the skill before working on a task the skill covers, and then follow the " +
			"instructions it returns.",
	}, func(ctx context.Context, in skillToolInput) (string, error) {
		loaded, err := registry.Load(in.Name)
		if err != nil {
			if errors.Is(err, ErrSkillNotFound) {
				return "", fmt.Errorf(
					"%w, the installed skills are: %s",
					err, strings.Join(registry.Names(), ", "),
				)
			}

			return "", err
		}

		slog.Debug("loaded a skill on the model's request", "skill", loaded.Name)

		return loaded.Instructions, nil
	})
	if err != nil {
		panic(err)
	}

	return tool
}
