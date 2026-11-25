package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/taichu-lang/raven-agents/apiserver/configs"
)

type ModelHandler struct {
	catalogs configs.ModelCatalogSlice
}

func NewModelHandler(catalogs configs.ModelCatalogSlice) *ModelHandler {
	models := make(configs.ModelCatalogSlice, 0, len(catalogs))
	for _, c := range catalogs {
		m := *c
		m.Provider = ""
		models = append(models, &m)
	}

	return &ModelHandler{
		catalogs: models,
	}
}

func (h *ModelHandler) AddRouters(router *gin.RouterGroup) {
	router.GET("/models", h.listModels)
}

func (h *ModelHandler) listModels(c *gin.Context) {
	c.JSON(http.StatusOK, h.catalogs)
}
