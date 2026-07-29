package adminhandler

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

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
	g.GET("/:id", h.Get)
}

func (h *Handler) List(c echo.Context) error {
	page, _ := strconv.Atoi(c.QueryParam("page"))
	perPage, _ := strconv.Atoi(c.QueryParam("per_page"))
	resp, err := h.svc.AdminList(c.Request().Context(), page, perPage)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

func (h *Handler) Get(c echo.Context) error {
	resp, err := h.svc.AdminGet(c.Request().Context(), c.Param("id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}
