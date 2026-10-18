package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/taichu-lang/raven-agents/apiserver/application"
	"github.com/taichu-lang/raven-agents/apiserver/handler/middlewares"
	"github.com/taichu-lang/raven-agents/apiserver/infrastructure"
	"github.com/taichu-lang/raven-agents/apiserver/infrastructure/configs"
)

func AddRouters(router *gin.Engine, cfg *configs.Config, store *infrastructure.Store) {
	// Registered on the engine so that preflight OPTIONS requests, which match no route, still pass through it.
	router.Use(middlewares.CORS())
	group := router.Group("/api/v1")

	chatService := application.NewChatService(cfg, store.Conversation)
	chatHandler := NewChatHandler(chatService)
	chatHandler.AddRouters(group)

	skillService := application.NewSkillService(store.Skill)
	skillHandler := NewSkillHandler(skillService)
	skillHandler.AddRouters(group)

	userService := application.NewUserService(store.User)
	userHandler := NewUserHandler(userService)
	userHandler.AddRouters(group)
}
