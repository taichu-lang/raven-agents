package handler

import (
	"io"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
	"github.com/taichu-lang/raven-agents/apiserver/application"
	"github.com/taichu-lang/raven-agents/apiserver/domain"
	"github.com/taichu-lang/raven-agents/internal/event"
)

type ChatHandler struct {
	service *application.ChatService
	logger  *slog.Logger
}

func NewChatHandler(srv *application.ChatService) *ChatHandler {
	return &ChatHandler{
		service: srv,
		logger:  slog.With("handler", "chat"),
	}
}

func (h *ChatHandler) AddRouters(router *gin.RouterGroup) {
	router.POST("/chat/:id", h.chat)
	router.GET("/conversations", h.getConversations)
}

func (h *ChatHandler) chat(c *gin.Context) {
	id := c.Param("id")

	var request domain.ChatRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}

	request.ConversationID = id
	h.logger.Debug("accept chat message", "conversation_id", id, "message", request)
	ctx := c.Request.Context()

	events, err := h.service.Chat(ctx, &request)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": err.Error()})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	// Stream flushes after every step. ctx is watched here rather than relying on the client-gone
	// check gin does between steps, which a step blocked on the channel would never reach.
	c.Stream(func(w io.Writer) bool {
		select {
		case e, ok := <-events:
			if !ok {
				return false
			}

			switch e.Name {
			case event.EventTurnStart:
				c.SSEvent(string(e.Name), e.Payload)

			case event.EventLLMDelta:
				fallthrough
			case event.EventLLMComplete:
				message := e.Payload.(*underlying.Message)
				c.SSEvent(string(e.Name), message)

			case event.EventTurnEnd:
				c.SSEvent(string(e.Name), "[DONE]")
			}

			return true

		case <-ctx.Done():
			return false
		}
	})
}
