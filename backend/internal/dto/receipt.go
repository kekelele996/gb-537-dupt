package dto

import (
	"encoding/json"
	"time"

	"pki-certificate-rollover-impact/backend/internal/constants"
	"pki-certificate-rollover-impact/backend/internal/model"
)

type SubmitReceiptRequest struct {
	SwitchedToNewRoot bool   `json:"switched_to_new_root"`
	Comment           string `json:"comment" validate:"max=1000"`
}

type ReviewReceiptRequest struct {
	Decision string `json:"decision" validate:"required,oneof=confirm reject"`
	Comment  string `json:"comment" validate:"max=1000"`
}

type ReceiptResponse struct {
	ID                    uint                          `json:"id"`
	ScenarioID            uint                          `json:"scenario_id"`
	ServiceID             uint                          `json:"service_id"`
	ServiceCode           string                        `json:"service_code"`
	ReportedTrustRefsJSON []uint                        `json:"reported_trust_refs_json"`
	SwitchedToNewRoot     bool                          `json:"switched_to_new_root"`
	ReceiptState          string                        `json:"receipt_state"`
	ReconciliationStatus  constants.ReconciliationStatus `json:"reconciliation_status"`
	FailureReason         string                        `json:"failure_reason"`
	ReportedBy            uint                          `json:"reported_by"`
	ReportedByName        string                        `json:"reported_by_name"`
	ReportedAt            time.Time                     `json:"reported_at"`
	ReviewedBy            *uint                         `json:"reviewed_by,omitempty"`
	ReviewedByName        string                        `json:"reviewed_by_name"`
	ReviewComment         string                        `json:"review_comment"`
	ReviewedAt            *time.Time                    `json:"reviewed_at,omitempty"`
	CreatedAt             time.Time                     `json:"created_at"`
	UpdatedAt             time.Time                     `json:"updated_at"`
}

type ReconciliationMismatch struct {
	ServiceID            uint                          `json:"service_id"`
	ServiceCode          string                        `json:"service_code"`
	SimulationPrediction string                        `json:"simulation_prediction"`
	ReceiptStatus        constants.ReconciliationStatus `json:"receipt_status"`
	ReceiptState         string                        `json:"receipt_state"`
	Detail               string                        `json:"detail"`
}

type ReconciliationSummary struct {
	ScenarioID      uint                       `json:"scenario_id"`
	TotalExpected   int                        `json:"total_expected"`
	PendingCount    int                        `json:"pending_count"`
	FailedCount     int                        `json:"failed_count"`
	SubmittedCount  int                        `json:"submitted_count"`
	SuspendedCount  int                        `json:"suspended_count"`
	ConfirmedCount  int                        `json:"confirmed_count"`
	RejectedCount   int                        `json:"rejected_count"`
	Ready           bool                       `json:"ready"`
	ReadyBlockers   []string                   `json:"ready_blockers"`
	Mismatches      []ReconciliationMismatch   `json:"mismatches"`
}

type ReceiptListResponse struct {
	Items         []ReceiptResponse       `json:"items"`
	Total         int64                   `json:"total"`
	Page          int                     `json:"page"`
	Size          int                     `json:"size"`
	Summary       ReconciliationSummary   `json:"summary"`
}

func NewReceiptResponse(receipt model.Receipt) ReceiptResponse {
	refs := []uint{}
	_ = json.Unmarshal([]byte(receipt.ReportedTrustRefsJSON), &refs)
	return ReceiptResponse{
		ID:                    receipt.ID,
		ScenarioID:            receipt.ScenarioID,
		ServiceID:             receipt.ServiceID,
		ServiceCode:           receipt.ServiceCode,
		ReportedTrustRefsJSON: refs,
		SwitchedToNewRoot:     receipt.SwitchedToNewRoot,
		ReceiptState:          receipt.ReceiptState,
		ReconciliationStatus:  constants.ReconcileReceipt(constants.ReceiptState(receipt.ReceiptState), receipt.SwitchedToNewRoot),
		FailureReason:         receipt.FailureReason,
		ReportedBy:            receipt.ReportedBy,
		ReportedByName:        receipt.ReportedByName,
		ReportedAt:            receipt.ReportedAt,
		ReviewedBy:            receipt.ReviewedBy,
		ReviewedByName:        receipt.ReviewedByName,
		ReviewComment:         receipt.ReviewComment,
		ReviewedAt:            receipt.ReviewedAt,
		CreatedAt:             receipt.CreatedAt,
		UpdatedAt:             receipt.UpdatedAt,
	}
}
