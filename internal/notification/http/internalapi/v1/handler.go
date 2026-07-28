// Package v1 implements the template-driven internal notification API:
//
//	POST /internal/v1/notifications          -> AcceptCommand
//	POST /internal/v1/direct-notifications   -> AcceptDirectCommand
package v1

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

// Register mounts routes on the /internal/v1 group.
func (h *Handler) Register(g *echo.Group) {
	g.POST("/notifications", h.CreateCommand)
	g.POST("/direct-notifications", h.CreateDirectCommand)
}

func (h *Handler) CreateCommand(c echo.Context) error {
	var req notificationdto.CommandRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	resp, code, err := h.svc.AcceptCommand(c.Request().Context(), req)
	if err != nil {
		return respondError(c, code, err)
	}
	return c.JSON(code, resp)
}

func (h *Handler) CreateDirectCommand(c echo.Context) error {
	var req notificationdto.DirectCommandRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	resp, code, err := h.svc.AcceptDirectCommand(c.Request().Context(), req)
	if err != nil {
		return respondError(c, code, err)
	}
	return c.JSON(code, resp)
}

// respondError renders the HTTP status code returned by the service
// verbatim (notably 409 on idempotency conflicts, which apperr.Handler's
// generic Kind→status mapping does not support).
func respondError(c echo.Context, code int, err error) error {
	if code == 0 {
		return err
	}
	if ve, ok := err.(*apperr.Error); ok {
		return c.JSON(code, map[string]any{"message": "validation failed", "fields": ve.Fields})
	}
	return c.JSON(code, map[string]string{"message": err.Error()})
}
