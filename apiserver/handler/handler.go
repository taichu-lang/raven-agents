package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/taichu-lang/raven-agents/apiserver/application/chat"
	chathandler "github.com/taichu-lang/raven-agents/apiserver/handler/chat"
	"github.com/taichu-lang/raven-agents/apiserver/infrastructure/configs"
)

func AddRouters(router *gin.Engine, cfg *configs.Config) {
	group := router.Group("/api/v1")

	chatService := chat.NewService(cfg)
	chatHandler := chathandler.NewHandler(chatService)
	chatHandler.AddRouters(group)
}
