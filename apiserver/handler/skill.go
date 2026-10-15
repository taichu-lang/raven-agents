package handler

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/taichu-lang/raven-agents/apiserver/application"
	"github.com/taichu-lang/raven-agents/apiserver/domain"
)

type SkillHandler struct {
	service *application.SkillService
	logger  *slog.Logger
}

func NewSkillHandler(service *application.SkillService) *SkillHandler {
	return &SkillHandler{
		service: service,
		logger:  slog.With("handler", "skill"),
	}
}

func (h *SkillHandler) AddRouters(router *gin.RouterGroup) {
	router.GET("/skills", h.listSkills)
}

func (h *SkillHandler) listSkills(c *gin.Context) {
	var request domain.SkillPaginationRequest
	if err := c.ShouldBindQuery(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}

	items, err := h.service.List(c.Request.Context(), request.From, request.Limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.ApiCodeSystem.WithError(err))
		return
	}

	c.JSON(http.StatusOK, domain.ApiCodeSuccess.WithData(items))
}
