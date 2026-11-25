package application

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path"
	"strconv"
	"text/template"

	"github.com/google/uuid"
	"github.com/taichu-lang/raven-agents/agent"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/agent/skill"
	"github.com/taichu-lang/raven-agents/apiserver/configs"
	"github.com/taichu-lang/raven-agents/apiserver/domain"
	"github.com/taichu-lang/raven-agents/apiserver/infrastructure"
	"github.com/taichu-lang/raven-agents/pkg/event"
	"github.com/taichu-lang/raven-agents/pkg/webcrawler"
	"github.com/taichu-lang/raven-agents/sandbox"
	"github.com/taichu-lang/raven-agents/sandbox/local"
	"github.com/taichu-lang/raven-agents/tool"
)

const EventConversationStart = "conversation.start"
const TemplateTitlePrompt = "title.prompt"

type ChatService struct {
	cfg             *configs.Config
	logger          *slog.Logger
	store           *infrastructure.ConversationStore
	skillsDir       string
	sandboxProvider sandbox.Provider
	promptTemplate  *template.Template
	models          map[string]*underlying.Catalog
	mcp             *MCPService
}

func NewChatService(
	cfg *configs.Config,
	catalogs configs.ModelCatalogSlice,
	store *infrastructure.ConversationStore,
	mcpService *MCPService,
) *ChatService {
	temp, err := template.New(TemplateTitlePrompt).Parse(cfg.Prompt.TitleGen)
	if err != nil {
		panic(err)
	}

	models := make(map[string]*underlying.Catalog)
	for _, catalog := range catalogs {
		models[catalog.ID] = catalog
	}

	return &ChatService{
		cfg:             cfg,
		logger:          slog.With("service", "chat"),
		store:           store,
		skillsDir:       path.Join(cfg.AppDir, ".agents", "skills"),
		sandboxProvider: local.NewProvider(),
		promptTemplate:  temp,
		models:          models,
		mcp:             mcpService,
	}
}

// Chat runs one turn of the conversation in the background and returns the stream of events it
// produces. The channel is closed once the turn is over, so the caller can simply range over it.
//
// The caller must keep receiving until the channel is closed, or cancel ctx. Both unblock the agent:
// the bus pushes back on a producer whose subscriber stopped reading, and only a cancelled ctx lets
// that producer give up.
func (s *ChatService) Chat(
	ctx context.Context,
	req *domain.ChatRequest,
	userID int64,
) (<-chan *event.Event, error) {
	if req.Model == "" {
		req.Model = s.cfg.DefaultModel
	}

	provider, catalog := s.getModelProvider(req.Model)
	if provider == nil {
		return nil, fmt.Errorf("model [%s] does not support", req.Model)
	}

	c, err := s.store.Get(ctx, req.ConversationID)
	if err != nil {
		return nil, fmt.Errorf("database error: %w", err)
	}

	if c == nil {
		c = &infrastructure.ConversationModel{
			ConversationID: req.ConversationID,
			UserID:         userID,
		}
		if err := s.store.Create(ctx, c); err != nil {
			return nil, fmt.Errorf("database error: %w", err)
		}
	}

	bus := event.NewMemoryBus()
	subscription := bus.Subscribe()

	titleGen := s.genConversationTitle(ctx, c, req.Text, req.Model, bus)
	options := &agent.AgentOptions{
		Name:           fmt.Sprintf("main-#%d", c.ID),
		UserID:         strconv.FormatInt(userID, 10),
		ConversationID: req.ConversationID,
		Store:          s.store.GetUnderlying(),
		LLM: &underlying.ProviderOptions{
			ApiKey:       provider.ApiKey,
			ChatURL:      provider.ChatURL,
			Instructions: s.cfg.Instructions,
			Catalog:      catalog,
			Alias:        provider.GetAlias(req.Model),
		},
		SkillDirs:       []string{s.skillsDir},
		SandboxProvider: s.sandboxProvider,
	}
	if s.cfg.WebCrawler != nil {
		options.Crawler = webcrawler.NewCrawler(s.cfg.WebCrawler)
	}

	// Plugins installed on the request context contribute their LLM middlewares; the chat service stays unaware
	// of what they are.
	mc := &agent.MiddlewareContext{
		UserID:         userID,
		ConversationID: req.ConversationID,
		Model:          catalog.ID,
	}
	for _, source := range agent.MiddlewareSourcesFromContext(ctx) {
		options.Middlewares = append(options.Middlewares, source.WithMiddlewares(ctx, mc)...)
	}

	// The MCP tools are deferred: they are dispatchable by name and found through the tool search, but never
	// advertised in the tools param. They are registered before the agent is built, as the tool search entry is
	// broadcast only while the registry has deferred tools.
	registry := tool.NewToolRegistry(nil)
	if s.mcp != nil {
		servers, tools := s.mcp.Resolve(ctx, userID)
		options.MCPServers = servers
		for _, t := range tools {
			registry.RegisterDeferred(t)
		}
	}

	a := agent.New(bus, registry, options)

	skills := s.resolveSkills(req.Metadata.Skills)
	go func() {
		// Unsubscribing closes the channel, which ends the consumer's receive loop.
		defer func() {
			titleGen.wait()
			bus.Unsubscribe(subscription.ID)
		}()
		runErr := s.runTurn(ctx, a, req, skills)
		if runErr != nil {
			// Report the failure down the stream so that it keeps its place in the order of the events the
			// consumer has already seen. ctx is detached because the failure is usually the cancellation
			// itself, and the event would then be dropped by the bus.
			bus.Emit(context.WithoutCancel(ctx), &event.Event{
				ID:      uuid.NewString(),
				Source:  event.SourceTypeRuntime,
				Name:    event.EventRuntimeError,
				Payload: runErr,
			})
		}

		// Shutdown still has to drain the tracer and close the store, which ctx being already cancelled
		// would skip.
		if err := a.Shutdown(context.WithoutCancel(ctx)); err != nil {
			slog.Warn("failed to shutdown agent", "conversation_id", req.ConversationID, "err", err)
		}
	}()

	return subscription.Channel, nil
}

func (s *ChatService) resolveSkills(skills []skill.Skill) []string {
	names := make([]string, 0, len(skills))
	for _, selected := range skills {
		names = append(names, selected.Name)
	}

	return names
}

// runTurn starts a new turn from the user's text, or resumes the paused one with the answers to the
// question the model asked.
func (s *ChatService) runTurn(
	ctx context.Context,
	a *agent.Agent,
	req *domain.ChatRequest,
	skills []string,
) error {
	if req.Resume != nil {
		answers, err := json.Marshal(req.Resume.Answers)
		if err != nil {
			return err
		}

		return a.Resume(ctx, []*underlying.ToolResultContent{
			underlying.NewToolResultContent(req.Resume.ID, req.Resume.Name, string(answers), false),
		})
	}

	messages := make([]*underlying.Message, 0, 1+len(skills))
	messages = append(messages, underlying.NewMessage(
		underlying.RoleUser,
		underlying.MessageContents{
			underlying.NewTextContent(req.Text),
		},
	))
	for _, skill := range skills {
		messages = append(messages, underlying.NewMessage(
			underlying.RoleUser,
			underlying.MessageContents{
				underlying.NewUnresolvedSkillContent(skill),
			},
		))
	}

	return a.Run(ctx, messages)
}

func (s *ChatService) getModelProvider(model string) (*configs.ModelProvider, *underlying.Catalog) {
	catalog, ok := s.models[model]
	if !ok {
		return nil, nil
	}

	for _, p := range s.cfg.Providers {
		if p.Name == catalog.Provider {
			return &p, catalog
		}
	}

	return nil, nil
}
