package main

import (
	"os"

	"github.com/gin-gonic/gin"
	"github.com/taichu-lang/raven-agents/apiserver/handler"
	"github.com/taichu-lang/raven-agents/apiserver/infrastructure/configs"
)

func main() {
	logger := initLogger()
	cfg := &configs.Config{}
	if err := cfg.Load(); err != nil {
		logger.Error("failed to resolve config", "err", err)
		os.Exit(1)
	}

	engine := gin.Default()
	handler.AddRouters(engine, cfg)
	if err := engine.Run("0.0.0.0:8001"); err != nil {
		logger.Error("failed to start server", "err", err)
		os.Exit(1)
	}
}
