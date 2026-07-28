package adminhandler

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// RegisterBatches mounts GET /admin/notification-batches/:batch_id.
func (h *Handler) RegisterBatches(g *echo.Group) {
	g.GET("/:batch_id", h.GetBatch)
}

func (h *Handler) GetBatch(c echo.Context) error {
	resp, err := h.svc.AdminGetBatch(c.Request().Context(), c.Param("batch_id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}
