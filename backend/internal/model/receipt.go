package model

import "time"

// Receipt is a service owner's reported switch-to-new-root progress for a
// single service within a rollover scenario. It is the service-side half of
// the reconciliation: the platform freezes the simulation, the service owner
// reports whether their trust set now includes the new root.
type Receipt struct {
	ID                    uint       `gorm:"primaryKey" json:"id"`
	ScenarioID            uint       `gorm:"not null;uniqueIndex:idx_receipt_scenario_service,priority:1" json:"scenario_id"`
	ServiceID             uint       `gorm:"not null;uniqueIndex:idx_receipt_scenario_service,priority:2" json:"service_id"`
	ServiceCode           string     `gorm:"size:80;not null" json:"service_code"`
	ReportedTrustRefsJSON string     `gorm:"type:text;not null" json:"reported_trust_refs_json"`
	SwitchedToNewRoot     bool       `gorm:"not null" json:"switched_to_new_root"`
	ReceiptState          string     `gorm:"size:24;not null;index" json:"receipt_state"`
	FailureReason         string     `gorm:"type:text" json:"failure_reason"`
	ReportedBy            uint       `gorm:"not null" json:"reported_by"`
	ReportedByName        string     `gorm:"size:80;not null" json:"reported_by_name"`
	ReportedAt            time.Time  `gorm:"not null" json:"reported_at"`
	ReviewedBy            *uint      `json:"reviewed_by,omitempty"`
	ReviewedByName        string     `gorm:"size:80" json:"reviewed_by_name"`
	ReviewComment         string     `gorm:"type:text" json:"review_comment"`
	ReviewedAt            *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt             time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt             time.Time  `gorm:"not null" json:"updated_at"`
}
