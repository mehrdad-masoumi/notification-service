package adminhandler

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	notificationdto "notification-service/internal/notification/dto"
	notificationservice "notification-service/internal/notification/service"
)

// TemplatesHandler exposes CRUD for notification templates under
// /admin/notification-templates.
type TemplatesHandler struct {
	svc *notificationservice.Service
}

func NewTemplatesHandler(svc *notificationservice.Service) *TemplatesHandler {
	return &TemplatesHandler{svc: svc}
}

func (h *TemplatesHandler) Register(g *echo.Group) {
	g.POST("", h.Create)
	g.GET("", h.List)
	g.GET("/:id", h.Get)
	g.PUT("/:id", h.Update)
	g.PATCH("/:id/status", h.SetStatus)
}

func (h *TemplatesHandler) Create(c echo.Context) error {
	var req notificationdto.TemplateCreateRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	resp, err := h.svc.AdminCreateTemplate(c.Request().Context(), req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, resp)
}

func (h *TemplatesHandler) List(c echo.Context) error {
	page, _ := strconv.Atoi(c.QueryParam("page"))
	perPage, _ := strconv.Atoi(c.QueryParam("per_page"))
	filter := notificationdto.TemplateListFilter{
		Code:    c.QueryParam("code"),
		Locale:  c.QueryParam("locale"),
		Channel: c.QueryParam("channel"),
		Page:    page,
		PerPage: perPage,
	}
	if v := c.QueryParam("enabled"); v != "" {
		enabled := v == "true" || v == "1"
		filter.Enabled = &enabled
	}
	resp, err := h.svc.AdminListTemplates(c.Request().Context(), filter)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

func (h *TemplatesHandler) Get(c echo.Context) error {
	resp, err := h.svc.AdminGetTemplate(c.Request().Context(), c.Param("id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

func (h *TemplatesHandler) Update(c echo.Context) error {
	var req notificationdto.TemplateUpdateRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	resp, err := h.svc.AdminUpdateTemplate(c.Request().Context(), c.Param("id"), req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

func (h *TemplatesHandler) SetStatus(c echo.Context) error {
	var req notificationdto.TemplateStatusRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if err := h.svc.AdminSetTemplateStatus(c.Request().Context(), c.Param("id"), req.Enabled); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}
