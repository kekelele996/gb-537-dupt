package constants

type ReceiptState string

const (
	ReceiptPending   ReceiptState = "pending"
	ReceiptFailed    ReceiptState = "failed"
	ReceiptSubmitted ReceiptState = "submitted"
	ReceiptSuspended ReceiptState = "suspended"
	ReceiptConfirmed ReceiptState = "confirmed"
	ReceiptRejected  ReceiptState = "rejected"
)

func ReceiptStateValues() []ReceiptState {
	return []ReceiptState{ReceiptPending, ReceiptFailed, ReceiptSubmitted, ReceiptSuspended, ReceiptConfirmed, ReceiptRejected}
}

// ReconciliationStatus is the computed outcome of matching a receipt against
// the simulation prediction for the same service. It is derived, never stored.
type ReconciliationStatus string

const (
	ReconciliationOK        ReconciliationStatus = "ok"
	ReconciliationBlocked   ReconciliationStatus = "blocked"
	ReconciliationSuspended ReconciliationStatus = "suspended"
	ReconciliationMissing   ReconciliationStatus = "missing"
	ReconciliationFailed    ReconciliationStatus = "failed"
)

// ReconcileReceipt maps a receipt state (and its switch claim) to the
// reconciliation outcome used by the ready gate and mismatch report.
func ReconcileReceipt(state ReceiptState, switched bool) ReconciliationStatus {
	switch state {
	case ReceiptConfirmed:
		return ReconciliationOK
	case ReceiptRejected:
		return ReconciliationBlocked
	case ReceiptSuspended:
		return ReconciliationSuspended
	case ReceiptFailed:
		return ReconciliationFailed
	case ReceiptSubmitted:
		if switched {
			return ReconciliationOK
		}
		return ReconciliationBlocked
	default:
		return ReconciliationMissing
	}
}
