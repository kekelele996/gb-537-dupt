package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"pki-certificate-rollover-impact/backend/internal/dto"
	"pki-certificate-rollover-impact/backend/internal/service"
	"pki-certificate-rollover-impact/backend/internal/util"
)

type ReceiptHandler struct {
	service *service.ReceiptService
}

func NewReceiptHandler(value *service.ReceiptService) *ReceiptHandler {
	return &ReceiptHandler{service: value}
}

func (h *ReceiptHandler) List(c *gin.Context) {
	scenarioID, err := util.ParseUintParam(c, "id")
	if err != nil {
		util.Fail(c, err)
		return
	}
	result, serviceErr := h.service.List(c.Request.Context(), scenarioID)
	respond(c, http.StatusOK, result, serviceErr)
}

func (h *ReceiptHandler) Backfill(c *gin.Context) {
	scenarioID, err := util.ParseUintParam(c, "id")
	if err != nil {
		util.Fail(c, err)
		return
	}
	result, serviceErr := h.service.Backfill(c.Request.Context(), scenarioID, mustActor(c), util.RequestID(c))
	respond(c, http.StatusOK, result, serviceErr)
}

func (h *ReceiptHandler) Submit(c *gin.Context) {
	scenarioID, err := util.ParseUintParam(c, "id")
	if err != nil {
		util.Fail(c, err)
		return
	}
	serviceID, err := util.ParseUintParam(c, "service_id")
	if err != nil {
		util.Fail(c, err)
		return
	}
	var request dto.SubmitReceiptRequest
	if !bindJSON(c, &request) {
		return
	}
	result, serviceErr := h.service.Submit(c.Request.Context(), scenarioID, serviceID, request, mustActor(c), util.RequestID(c))
	respond(c, http.StatusOK, result, serviceErr)
}

func (h *ReceiptHandler) Retry(c *gin.Context) {
	scenarioID, err := util.ParseUintParam(c, "id")
	if err != nil {
		util.Fail(c, err)
		return
	}
	serviceID, err := util.ParseUintParam(c, "service_id")
	if err != nil {
		util.Fail(c, err)
		return
	}
	var request dto.SubmitReceiptRequest
	if !bindJSON(c, &request) {
		return
	}
	result, serviceErr := h.service.Retry(c.Request.Context(), scenarioID, serviceID, request, mustActor(c), util.RequestID(c))
	respond(c, http.StatusOK, result, serviceErr)
}

func (h *ReceiptHandler) Review(c *gin.Context) {
	scenarioID, err := util.ParseUintParam(c, "id")
	if err != nil {
		util.Fail(c, err)
		return
	}
	serviceID, err := util.ParseUintParam(c, "service_id")
	if err != nil {
		util.Fail(c, err)
		return
	}
	var request dto.ReviewReceiptRequest
	if !bindJSON(c, &request) {
		return
	}
	result, serviceErr := h.service.Review(c.Request.Context(), scenarioID, serviceID, request, mustActor(c), util.RequestID(c))
	respond(c, http.StatusOK, result, serviceErr)
}
