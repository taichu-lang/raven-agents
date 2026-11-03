package main

import (
	"context"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/taichu-lang/raven-agents/apiserver/configs"
	"github.com/taichu-lang/raven-agents/apiserver/handler"
	"github.com/taichu-lang/raven-agents/apiserver/infrastructure"
	"github.com/taichu-lang/raven-agents/pkg/observability"
)

func main() {
	logger := initLogger()
	cfg, err := configs.LoadConfig()
	if err != nil {
		logger.Error("failed to resolve config", "err", err)
		os.Exit(1)
	}

	catalogs, err := configs.LoadModelCatalog()
	if err != nil {
		logger.Error("failed to resolve model catalog", "err", err)
		os.Exit(1)
	}

	ctx := context.Background()

	store := infrastructure.NewStore(cfg.Store)
	defer store.Close()

	tp, err := observability.NewTracerProvider(ctx)
	if err != nil {
		logger.Error("failed to init tracer provider", "err", err)
		os.Exit(1)
	}

	defer tp.Shutdown(ctx)

	engine := gin.Default()
	handler.AddRouters(engine, cfg, catalogs, store)
	if err := engine.Run("0.0.0.0:8001"); err != nil {
		logger.Error("failed to start server", "err", err)
		os.Exit(1)
	}
}
