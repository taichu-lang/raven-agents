package main

import (
	"context"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/taichu-lang/raven-agents/apiserver/configs"
	"github.com/taichu-lang/raven-agents/apiserver/handler"
	"github.com/taichu-lang/raven-agents/apiserver/infrastructure"
	"github.com/taichu-lang/raven-agents/mcp"
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

	// MCP sessions outlive the requests that opened them; stdio servers are child processes closed here.
	mcpManager := mcp.NewManager()
	defer mcpManager.Close()

	engine := gin.Default()
	if err := handler.AddRouters(engine, cfg, catalogs, store, mcpManager); err != nil {
		logger.Error("failed to add routers", "err", err)
		os.Exit(1)
	}

	if err := engine.Run("0.0.0.0:8001"); err != nil {
		logger.Error("failed to start server", "err", err)
		os.Exit(1)
	}
}
