package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/taichu-lang/raven-agents/apiserver/application"
	"github.com/taichu-lang/raven-agents/apiserver/domain"
)

type UserHandler struct {
	service *application.UserService
	logger  *slog.Logger
}

func NewUserHandler(service *application.UserService) *UserHandler {
	return &UserHandler{
		service: service,
		logger:  slog.With("handler", "user"),
	}
}

func (h *UserHandler) AddRouters(router *gin.RouterGroup) {
	router.POST("/auth/register", h.register)
	router.POST("/auth/login", h.login)
}

func (h *UserHandler) register(c *gin.Context) {
	var request domain.RegisterRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, domain.ApiCodeInput.WithError(err))
		return
	}

	info, err := h.service.Register(c.Request.Context(), &request)
	if err != nil {
		if errors.Is(err, domain.ErrEmailRegistered) {
			c.JSON(http.StatusConflict, domain.ApiCodeInput.WithError(err))
			return
		}

		h.logger.Error("register user", "error", err)
		c.JSON(http.StatusInternalServerError, domain.ApiCodeSystem.WithError(err))
		return
	}

	c.JSON(http.StatusOK, domain.ApiCodeSuccess.WithData(info))
}

func (h *UserHandler) login(c *gin.Context) {
	var request domain.LoginRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, domain.ApiCodeInput.WithError(err))
		return
	}

	info, err := h.service.Login(c.Request.Context(), &request)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidCredentials):
			c.JSON(http.StatusUnauthorized, domain.ApiCodeInput.WithError(err))
		case errors.Is(err, domain.ErrUserDisabled):
			c.JSON(http.StatusForbidden, domain.ApiCodeInput.WithError(err))
		default:
			h.logger.Error("login user", "error", err)
			c.JSON(http.StatusInternalServerError, domain.ApiCodeSystem.WithError(err))
		}

		return
	}

	c.JSON(http.StatusOK, domain.ApiCodeSuccess.WithData(info))
}
