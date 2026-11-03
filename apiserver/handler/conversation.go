package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/taichu-lang/raven-agents/apiserver/domain"
	"github.com/taichu-lang/raven-agents/apiserver/handler/middlewares"
)

func (h *ChatHandler) getConversations(c *gin.Context) {
	var request domain.ConversationPageRequest
	if err := c.ShouldBindQuery(&request); err != nil {
		c.JSON(http.StatusBadRequest, domain.ApiCodeInput.WithError(err))
		return
	}

	userID, _ := middlewares.UserIDFromContext(c)
	items, err := h.service.GetConversations(c.Request.Context(), userID, request.From, request.Limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.ApiCodeSystem.WithError(err))
		return
	}

	c.JSON(http.StatusOK, domain.ApiCodeSuccess.WithData(items))
}

func (h *ChatHandler) getConversation(c *gin.Context) {
	id := c.Param("id")
	conversation, err := h.service.GetConversation(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.ApiCodeSystem.WithError(err))
		return
	}

	if conversation == nil {
		c.JSON(http.StatusNotFound, domain.ApiCodeInput.WithData("no conversation"))
		return
	}

	c.JSON(http.StatusOK, domain.ApiCodeSuccess.WithData(conversation))
}
