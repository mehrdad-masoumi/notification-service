package userhandler

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/mehrdad-masoumi/go-packages/auth"
	notificationservice "notification-service/internal/notification/service"
)

type Handler struct {
	svc *notificationservice.Service
}

func New(svc *notificationservice.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(g *echo.Group) {
	g.GET("", h.List)
	g.GET("/unread-count", h.UnreadCount)
	g.PATCH("/:id/read", h.MarkRead)
	g.POST("/read-all", h.MarkAllRead)
}

func (h *Handler) List(c echo.Context) error {
	claims, err := auth.GetClaims(c)
	if err != nil {
		return err
	}
	page, _ := strconv.Atoi(c.QueryParam("page"))
	perPage, _ := strconv.Atoi(c.QueryParam("per_page"))
	resp, err := h.svc.ListUser(c.Request().Context(), claims.UserID, page, perPage)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

func (h *Handler) UnreadCount(c echo.Context) error {
	claims, err := auth.GetClaims(c)
	if err != nil {
		return err
	}
	resp, err := h.svc.UnreadCount(c.Request().Context(), claims.UserID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

func (h *Handler) MarkRead(c echo.Context) error {
	claims, err := auth.GetClaims(c)
	if err != nil {
		return err
	}
	if err := h.svc.MarkRead(c.Request().Context(), claims.UserID, c.Param("id")); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) MarkAllRead(c echo.Context) error {
	claims, err := auth.GetClaims(c)
	if err != nil {
		return err
	}
	if err := h.svc.MarkAllRead(c.Request().Context(), claims.UserID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}
