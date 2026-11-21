package handler

import (
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/taichu-lang/raven-agents/agent"
	"github.com/taichu-lang/raven-agents/apiserver/application"
	"github.com/taichu-lang/raven-agents/apiserver/domain"
	"github.com/taichu-lang/raven-agents/apiserver/handler/middlewares"
	"github.com/taichu-lang/raven-agents/pkg/database"
	"github.com/taichu-lang/raven-agents/pkg/event"
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
	router.GET("/conversations/:id", h.getConversation)
}

func (h *ChatHandler) chat(c *gin.Context) {
	id := c.Param("id")

	var request domain.ChatRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, domain.ApiCodeInput.WithError(err))
		return
	}

	request.ConversationID = id
	h.logger.Debug("accept chat message", "conversation_id", id, "message", request)

	userID, _ := middlewares.UserIDFromContext(c)
	ctx := c.Request.Context()
	events, err := h.service.Chat(ctx, &request, userID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, agent.ErrSkillNotFound) {
			status = http.StatusBadRequest
		}

		c.JSON(status, domain.ApiCodeSystem.WithError(err))
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

			// TODO(Leo): handle error event.
			switch e.Name {
			case application.EventConversationStart,
				event.EventTurnStart,
				event.EventLLMDelta,
				event.EventLLMComplete,
				event.EventToolCall,
				event.EventToolResult,
				event.EventHumanApproval,
				event.EventLLMReasoningDelta:
				c.SSEvent(string(e.Name), e.Payload)

			case event.EventTurnEnd:
				turn := e.Payload.(*database.TurnModel)
				c.SSEvent(string(e.Name), domain.TurnFromModel(turn))
			}

			return true

		case <-ctx.Done():
			return false
		}
	})
}
