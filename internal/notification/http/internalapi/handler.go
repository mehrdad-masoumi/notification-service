package internalhandler

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/mehrdad-masoumi/go-packages/apperr"
	notificationdto "notification-service/internal/notification/dto"
	notificationservice "notification-service/internal/notification/service"
)

type Handler struct {
	svc *notificationservice.Service
}

func New(svc *notificationservice.Service) *Handler {
	return &Handler{svc: svc}
}

// Register mounts the deprecated free-text internal create endpoint.
// Prefer /internal/v1/notifications (see http/internalapi/v1) for new
// integrations: it is template-driven and supports locale/variables.
func (h *Handler) Register(g *echo.Group) {
	g.POST("", h.Create)
}

// Create handles the deprecated POST /internal/notifications endpoint.
//
// Deprecated: use POST /internal/v1/notifications instead.
func (h *Handler) Create(c echo.Context) error {
	var req notificationdto.InternalCreateRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	resp, code, err := h.svc.InternalCreate(c.Request().Context(), req)
	if err != nil {
		if code == 0 {
			return err
		}
		if ve, ok := err.(*apperr.Error); ok {
			return c.JSON(code, map[string]any{"message": "validation failed", "fields": ve.Fields})
		}
		return c.JSON(code, map[string]string{"message": err.Error()})
	}
	return c.JSON(code, resp)
}
