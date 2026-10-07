package router

import (
	"github.com/gin-gonic/gin"
	"pki-certificate-rollover-impact/backend/internal/constants"
	"pki-certificate-rollover-impact/backend/internal/handler"
	"pki-certificate-rollover-impact/backend/internal/middleware"
)

func RegisterReceiptRoutes(api *gin.RouterGroup, h *handler.ReceiptHandler) {
	group := api.Group("/rollover-scenarios/:id/receipts")
	group.GET("", middleware.RequirePermission(constants.PermissionRead), h.List)
	group.POST("/backfill", middleware.RequirePermission(constants.PermissionScenarioWrite), h.Backfill)
	group.POST("/:service_id/submit", middleware.RequirePermission(constants.PermissionReceiptSubmit), h.Submit)
	group.POST("/:service_id/retry", middleware.RequirePermission(constants.PermissionReceiptSubmit), h.Retry)
	group.POST("/:service_id/review", middleware.RequirePermission(constants.PermissionReceiptReview), h.Review)
}
