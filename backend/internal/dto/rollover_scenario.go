package dto

import (
	"encoding/json"
	"time"

	"pki-certificate-rollover-impact/backend/internal/algorithm"
	"pki-certificate-rollover-impact/backend/internal/model"
)

type CreateRolloverScenarioRequest struct {
	Name              string    `json:"name" validate:"required,min=3,max=180"`
	OldAnchorID       uint      `json:"old_anchor_id" validate:"required"`
	NewAnchorID       uint      `json:"new_anchor_id" validate:"required"`
	OverlapStart      time.Time `json:"overlap_start" validate:"required"`
	OverlapEnd        time.Time `json:"overlap_end" validate:"required"`
	CandidateChainIDs []uint    `json:"candidate_chain_ids" validate:"required,min=1,max=32,dive,gt=0"`
	SimulationTime    time.Time `json:"simulation_time" validate:"required"`
}
type RolloverScenarioTransitionRequest struct {
	ToState string `json:"to_state" validate:"required,oneof=draft ready executing verified rollback"`
	Comment string `json:"comment" validate:"max=1000"`
}
type RolloverScenarioQuery struct {
	State     string
	CreatedBy uint
	Page      int
	PageSize  int
}

type SubmitMigrationReceiptRequest struct {
	ServiceID      uint   `json:"service_id" validate:"required"`
	MigrationState string `json:"migration_state" validate:"required,oneof=not_started in_progress migrated"`
	TrustAnchorIDs []uint `json:"trust_anchor_ids" validate:"max=32,dive,gt=0"`
	Note           string `json:"note" validate:"max=1000"`
}
type RetryMigrationReceiptRequest struct {
	Note string `json:"note" validate:"max=1000"`
}
type ReviewMigrationReceiptRequest struct {
	Decision string `json:"decision" validate:"required,oneof=cleared rejected"`
	Comment  string `json:"comment" validate:"max=1000"`
}

type MigrationReceiptResponse struct {
	ID                uint       `json:"id"`
	ScenarioID        uint       `json:"scenario_id"`
	ServiceID         uint       `json:"service_id"`
	ServiceCode       string     `json:"service_code"`
	MigrationState    string     `json:"migration_state"`
	TrustAnchorIDs    []uint     `json:"trust_anchor_ids"`
	Note              string     `json:"note"`
	DeliveryState     string     `json:"delivery_state"`
	DeliveryAttempts  int        `json:"delivery_attempts"`
	LastDeliveryError string     `json:"last_delivery_error"`
	ReviewStatus      string     `json:"review_status"`
	ReviewedByName    string     `json:"reviewed_by_name"`
	ReviewedAt        *time.Time `json:"reviewed_at,omitempty"`
	ReviewComment     string     `json:"review_comment"`
	LegacyBackfilled  bool       `json:"legacy_backfilled"`
	ReportedByName    string     `json:"reported_by_name"`
	DeliveredAt       *time.Time `json:"delivered_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type ServiceReconciliation struct {
	ServiceID       uint                      `json:"service_id"`
	ServiceCode     string                    `json:"service_code"`
	PredictedBroken bool                      `json:"predicted_broken"`
	HasReceipt      bool                      `json:"has_receipt"`
	Receipt         *MigrationReceiptResponse `json:"receipt,omitempty"`
	Status          string                    `json:"status"`
	Reason          string                    `json:"reason"`
}
type ReceiptReconciliationResponse struct {
	ScenarioID    uint                    `json:"scenario_id"`
	ScenarioState string                  `json:"scenario_state"`
	TotalServices int                     `json:"total_services"`
	Counts        map[string]int          `json:"counts"`
	ReadyToMark   bool                    `json:"ready_to_mark"`
	Blocking      []ServiceReconciliation `json:"blocking"`
	Items         []ServiceReconciliation `json:"items"`
}

type RolloverScenarioResponse struct {
	ID                   uint                          `json:"id"`
	Name                 string                        `json:"name"`
	OldAnchorID          uint                          `json:"old_anchor_id"`
	NewAnchorID          uint                          `json:"new_anchor_id"`
	OldAnchor            *TrustAnchorResponse          `json:"old_anchor,omitempty"`
	NewAnchor            *TrustAnchorResponse          `json:"new_anchor,omitempty"`
	OverlapStart         time.Time                     `json:"overlap_start"`
	OverlapEnd           time.Time                     `json:"overlap_end"`
	CandidateChainIDs    []uint                        `json:"candidate_chain_ids"`
	AlgorithmVersion     string                        `json:"algorithm_version"`
	InputHash            string                        `json:"input_hash"`
	SimulationTime       time.Time                     `json:"simulation_time"`
	AffectedServicesJSON []algorithm.AffectedService   `json:"affected_services_json"`
	BrokenPathsJSON      []algorithm.BrokenPath        `json:"broken_paths_json"`
	PathEvidenceJSON     []algorithm.TimepointEvidence `json:"path_evidence_json"`
	ScenarioState        string                        `json:"scenario_state"`
	Explanation          string                        `json:"explanation"`
	CreatedBy            uint                          `json:"created_by"`
	CreatedByName        string                        `json:"created_by_name"`
	VerifiedBy           *uint                         `json:"verified_by,omitempty"`
	VerifiedByName       string                        `json:"verified_by_name"`
	ReplayVerified       bool                          `json:"replay_verified"`
	DurationMS           int64                         `json:"duration_ms"`
	RollbackRecord       string                        `json:"rollback_record"`
	CreatedAt            time.Time                     `json:"created_at"`
	UpdatedAt            time.Time                     `json:"updated_at"`
}
type RolloverScenarioListResponse struct {
	Items []RolloverScenarioResponse `json:"items"`
	Total int64                      `json:"total"`
	Page  int                        `json:"page"`
	Size  int                        `json:"size"`
}

func NewRolloverScenarioResponse(scenario model.RolloverScenario, now time.Time) RolloverScenarioResponse {
	candidateIDs := []uint{}
	affected := []algorithm.AffectedService{}
	paths := []algorithm.BrokenPath{}
	evidence := []algorithm.TimepointEvidence{}
	_ = json.Unmarshal([]byte(scenario.CandidateChainIDs), &candidateIDs)
	_ = json.Unmarshal([]byte(scenario.AffectedServicesJSON), &affected)
	_ = json.Unmarshal([]byte(scenario.BrokenPathsJSON), &paths)
	_ = json.Unmarshal([]byte(scenario.PathEvidenceJSON), &evidence)
	response := RolloverScenarioResponse{ID: scenario.ID, Name: scenario.Name, OldAnchorID: scenario.OldAnchorID, NewAnchorID: scenario.NewAnchorID, OverlapStart: scenario.OverlapStart, OverlapEnd: scenario.OverlapEnd, CandidateChainIDs: candidateIDs, AlgorithmVersion: scenario.AlgorithmVersion, InputHash: scenario.InputHash, SimulationTime: scenario.SimulationTime, AffectedServicesJSON: affected, BrokenPathsJSON: paths, PathEvidenceJSON: evidence, ScenarioState: scenario.ScenarioState, Explanation: scenario.Explanation, CreatedBy: scenario.CreatedBy, CreatedByName: scenario.CreatedByName, VerifiedBy: scenario.VerifiedBy, VerifiedByName: scenario.VerifiedByName, ReplayVerified: scenario.ReplayVerified, DurationMS: scenario.DurationMS, RollbackRecord: scenario.RollbackRecord, CreatedAt: scenario.CreatedAt, UpdatedAt: scenario.UpdatedAt}
	if scenario.OldAnchor.ID != 0 {
		anchor := NewTrustAnchorResponse(scenario.OldAnchor, 0, now)
		response.OldAnchor = &anchor
	}
	if scenario.NewAnchor.ID != 0 {
		anchor := NewTrustAnchorResponse(scenario.NewAnchor, 0, now)
		response.NewAnchor = &anchor
	}
	return response
}

func NewMigrationReceiptResponse(receipt model.MigrationReceipt) MigrationReceiptResponse {
	anchorIDs := []uint{}
	_ = json.Unmarshal([]byte(receipt.TrustAnchorIDs), &anchorIDs)
	return MigrationReceiptResponse{ID: receipt.ID, ScenarioID: receipt.ScenarioID, ServiceID: receipt.ServiceID, ServiceCode: receipt.ServiceCode, MigrationState: receipt.MigrationState, TrustAnchorIDs: anchorIDs, Note: receipt.Note, DeliveryState: receipt.DeliveryState, DeliveryAttempts: receipt.DeliveryAttempts, LastDeliveryError: receipt.LastDeliveryError, ReviewStatus: receipt.ReviewStatus, ReviewedByName: receipt.ReviewedByName, ReviewedAt: receipt.ReviewedAt, ReviewComment: receipt.ReviewComment, LegacyBackfilled: receipt.LegacyBackfilled, ReportedByName: receipt.ReportedByName, DeliveredAt: receipt.DeliveredAt, CreatedAt: receipt.CreatedAt, UpdatedAt: receipt.UpdatedAt}
}
