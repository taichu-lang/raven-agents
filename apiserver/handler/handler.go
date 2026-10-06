package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/taichu-lang/raven-agents/apiserver/application"
	"github.com/taichu-lang/raven-agents/apiserver/infrastructure"
	"github.com/taichu-lang/raven-agents/apiserver/infrastructure/configs"
)

func AddRouters(router *gin.Engine, cfg *configs.Config, store *infrastructure.Store) {
	group := router.Group("/api/v1")

	chatService := application.NewChatService(cfg, store.Conversation)
	chatHandler := NewChatHandler(chatService)
	chatHandler.AddRouters(group)
}
