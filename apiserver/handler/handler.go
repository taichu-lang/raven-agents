package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/taichu-lang/raven-agents/apiserver/application"
	"github.com/taichu-lang/raven-agents/apiserver/configs"
	"github.com/taichu-lang/raven-agents/apiserver/handler/middlewares"
	"github.com/taichu-lang/raven-agents/apiserver/infrastructure"
	"github.com/taichu-lang/raven-agents/mcp"
)

func AddRouters(
	router *gin.Engine,
	cfg *configs.Config,
	catalogs configs.ModelCatalogSlice,
	store *infrastructure.Store,
	mcpManager *mcp.Manager,
) error {
	// Registered on the engine so that preflight OPTIONS requests, which match no route, still pass through it.
	router.Use(middlewares.CORS())
	group := router.Group("/api/v1")

	modelHandler := NewModelHandler(catalogs)
	modelHandler.AddRouters(group)

	mcpService, err := application.NewMCPService(store.MCP, mcpManager, cfg.MCP.CredentialKey)
	if err != nil {
		return err
	}

	chatService := application.NewChatService(cfg, catalogs, store.Conversation, mcpService)
	chatHandler := NewChatHandler(chatService)
	chatHandler.AddRouters(group)

	skillService := application.NewSkillService(store.Skill)
	skillHandler := NewSkillHandler(skillService)
	skillHandler.AddRouters(group)

	return nil
}
