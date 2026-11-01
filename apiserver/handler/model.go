package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/taichu-lang/raven-agents/apiserver/infrastructure/configs"
)

type ModelHandler struct {
	catalogs configs.ModelCatalogSlice
}

func NewModelHandler(catalogs configs.ModelCatalogSlice) *ModelHandler {
	return &ModelHandler{
		catalogs: catalogs,
	}
}

func (h *ModelHandler) AddRouters(router *gin.RouterGroup) {
	router.GET("/models", h.listModels)
}

func (h *ModelHandler) listModels(c *gin.Context) {
	c.JSON(http.StatusOK, h.catalogs)
}
