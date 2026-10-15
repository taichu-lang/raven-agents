package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/taichu-lang/raven-agents/apiserver/domain"
)

func (h *ChatHandler) getConversations(c *gin.Context) {
	var request domain.ConversationPageRequest
	if err := c.ShouldBindQuery(&request); err != nil {
		c.JSON(http.StatusBadRequest, domain.ApiCodeInput.WithError(err))
		return
	}

	// Authentication is not wired yet, the chat flow also owns its conversations as user 1.
	items, err := h.service.GetConversations(c.Request.Context(), 1, request.From, request.Limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.ApiCodeSystem.WithError(err))
		return
	}

	c.JSON(http.StatusOK, domain.ApiCodeSuccess.WithData(items))
}
