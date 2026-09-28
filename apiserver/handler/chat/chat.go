package chat

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/taichu-lang/raven-agents/apiserver/application/chat"
	chatdomain "github.com/taichu-lang/raven-agents/apiserver/domain/chat"
)

type Handler struct {
	service *chat.Service
	logger  *slog.Logger
}

func NewHandler(srv *chat.Service) *Handler {
	return &Handler{
		service: srv,
		logger:  slog.With("handler", "chat"),
	}
}

func (h *Handler) AddRouters(router *gin.RouterGroup) {
	router.POST("/chat/:id", h.chat)
}

func (h *Handler) chat(c *gin.Context) {
	id := c.Param("id")

	var request chatdomain.Request
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}

	h.logger.Debug("accept chat message", "conversation_id", id, "message", request)
	ctx := c.Request.Context()
	meta, resp := h.service.Chat(ctx, &request)
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	flusher := c.Writer.(http.Flusher)

	write := func(event string, data interface{}) {
		c.SSEvent(event, data)
		flusher.Flush()
	}

	write(chatdomain.EventTurnStart, meta)
	for chunk, err := range resp {
		if ctx.Err() != nil {
			// Client disconnected.
			return
		}

		if err != nil {
			write("error", err.Error())
			return
		}

		write(string(chunk.Type), chunk)
	}

	write(chatdomain.EventDone, "[DONE]")
}
