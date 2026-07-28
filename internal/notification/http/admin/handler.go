package adminhandler

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/mehrdad-masoumi/go-packages/auth"
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
	g.GET("", h.List)
	g.GET("/:id", h.Get)
}

func (h *Handler) Create(c echo.Context) error {
	claims, err := auth.GetClaims(c)
	if err != nil {
		return err
	}
	var req notificationdto.AdminCreateRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	resp, err := h.svc.AdminCreate(c.Request().Context(), req, claims.UserID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusAccepted, resp)
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
