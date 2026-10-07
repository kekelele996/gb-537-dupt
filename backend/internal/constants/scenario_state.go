package constants

type ScenarioState string

const (
	ScenarioDraft     ScenarioState = "draft"
	ScenarioSimulated ScenarioState = "simulated"
	ScenarioReady     ScenarioState = "ready"
	ScenarioExecuting ScenarioState = "executing"
	ScenarioVerified  ScenarioState = "verified"
	ScenarioRollback  ScenarioState = "rollback"
)

func ScenarioStateValues() []ScenarioState {
	return []ScenarioState{ScenarioDraft, ScenarioSimulated, ScenarioReady, ScenarioExecuting, ScenarioVerified, ScenarioRollback}
}

func CanTransitionScenario(from, to ScenarioState) bool {
	allowed := map[ScenarioState][]ScenarioState{
		ScenarioDraft:     {ScenarioSimulated},
		ScenarioSimulated: {ScenarioReady, ScenarioDraft},
		ScenarioReady:     {ScenarioExecuting, ScenarioDraft},
		ScenarioExecuting: {ScenarioVerified, ScenarioRollback},
	}
	for _, candidate := range allowed[from] {
		if candidate == to {
			return true
		}
	}
	return false
}

// MigrationState is the owner-reported cutover progress for one service.
type MigrationState string

const (
	MigrationNotStarted MigrationState = "not_started"
	MigrationInProgress MigrationState = "in_progress"
	MigrationMigrated   MigrationState = "migrated"
)

func MigrationStateValues() []MigrationState {
	return []MigrationState{MigrationNotStarted, MigrationInProgress, MigrationMigrated}
}

// DeliveryState is the per-receipt reporting channel; a failure is isolated
// and retried independently of other services.
type DeliveryState string

const (
	DeliveryPending   DeliveryState = "pending"
	DeliveryDelivered DeliveryState = "delivered"
	DeliveryFailed    DeliveryState = "failed"
)

func DeliveryStateValues() []DeliveryState {
	return []DeliveryState{DeliveryPending, DeliveryDelivered, DeliveryFailed}
}

// ReviewStatus covers independent review of a report that contradicts the
// frozen simulation (predicted broken but claimed migrated).
type ReviewStatus string

const (
	ReviewNotRequired ReviewStatus = "not_required"
	ReviewPending     ReviewStatus = "pending"
	ReviewCleared     ReviewStatus = "cleared"
	ReviewRejected    ReviewStatus = "rejected"
)

func ReviewStatusValues() []ReviewStatus {
	return []ReviewStatus{ReviewNotRequired, ReviewPending, ReviewCleared, ReviewRejected}
}

// ReconciliationStatus compares the frozen simulation against the owner receipt.
type ReconciliationStatus string

const (
	ReconciliationReconciled            ReconciliationStatus = "reconciled"
	ReconciliationMismatchPendingReview ReconciliationStatus = "mismatch_pending_review"
	ReconciliationOutstanding           ReconciliationStatus = "outstanding"
	ReconciliationLegacyMissing         ReconciliationStatus = "legacy_missing"
	ReconciliationDeliveryFailed        ReconciliationStatus = "delivery_failed"
)

func ReconciliationStatusValues() []ReconciliationStatus {
	return []ReconciliationStatus{ReconciliationReconciled, ReconciliationMismatchPendingReview, ReconciliationOutstanding, ReconciliationLegacyMissing, ReconciliationDeliveryFailed}
}

// ReconcileReceipt derives per-service reconciliation. A backfilled placeholder
// the owner has not replaced is reported as reported=false so it stays listed
// separately as legacy_missing.
func ReconcileReceipt(broken, legacyBackfilled, reported bool, migrationState, deliveryState, reviewStatus string) ReconciliationStatus {
	if !reported {
		if legacyBackfilled {
			return ReconciliationLegacyMissing
		}
		return ReconciliationOutstanding
	}
	if deliveryState == string(DeliveryFailed) {
		return ReconciliationDeliveryFailed
	}
	if migrationState != string(MigrationMigrated) {
		return ReconciliationOutstanding
	}
	if broken && reviewStatus != string(ReviewCleared) {
		return ReconciliationMismatchPendingReview
	}
	return ReconciliationReconciled
}

// ReadyBlocked reports whether the status blocks simulated -> ready.
func (status ReconciliationStatus) ReadyBlocked() bool {
	switch status {
	case ReconciliationOutstanding, ReconciliationLegacyMissing, ReconciliationDeliveryFailed, ReconciliationMismatchPendingReview:
		return true
	default:
		return false
	}
}
