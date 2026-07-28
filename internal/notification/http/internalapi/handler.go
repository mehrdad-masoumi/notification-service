package internalhandler

import (
	"net/http"

	"github.com/labstack/echo/v4"

	notificationdto "notification-service/internal/notification/dto"
	notificationservice "notification-service/internal/notification/service"
)

type Handler struct {
	svc *notificationservice.Service
}

func New(svc *notificationservice.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(g *echo.Group) {
	g.POST("", h.Create)
}

func (h *Handler) Create(c echo.Context) error {
	var req notificationdto.InternalCreateRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	resp, code, err := h.svc.InternalCreate(c.Request().Context(), req)
	if err != nil {
		return err
	}
	return c.JSON(code, resp)
}
