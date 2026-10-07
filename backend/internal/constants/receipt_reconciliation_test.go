package constants

import "testing"

func TestReconcileReceiptMatrix(t *testing.T) {
	cases := []struct {
		name       string
		broken     bool
		legacy     bool
		reported   bool
		migration  string
		delivery   string
		review     string
		want       ReconciliationStatus
		readyBlock bool
	}{
		{name: "no receipt normal service", broken: false, legacy: false, reported: false, want: ReconciliationOutstanding, readyBlock: true},
		{name: "legacy simulation missing receipt", broken: false, legacy: true, reported: false, want: ReconciliationLegacyMissing, readyBlock: true},
		{name: "not finished migration", broken: false, reported: true, migration: string(MigrationInProgress), delivery: string(DeliveryDelivered), want: ReconciliationOutstanding, readyBlock: true},
		{name: "broken but claimed migrated held for review", broken: true, reported: true, migration: string(MigrationMigrated), delivery: string(DeliveryDelivered), review: string(ReviewNotRequired), want: ReconciliationMismatchPendingReview, readyBlock: true},
		{name: "broken claimed migrated pending review", broken: true, reported: true, migration: string(MigrationMigrated), delivery: string(DeliveryDelivered), review: string(ReviewPending), want: ReconciliationMismatchPendingReview, readyBlock: true},
		{name: "broken claimed migrated cleared by reviewer", broken: true, reported: true, migration: string(MigrationMigrated), delivery: string(DeliveryDelivered), review: string(ReviewCleared), want: ReconciliationReconciled, readyBlock: false},
		{name: "not broken and migrated", broken: false, reported: true, migration: string(MigrationMigrated), delivery: string(DeliveryDelivered), review: string(ReviewNotRequired), want: ReconciliationReconciled, readyBlock: false},
		{name: "delivery failed isolated", broken: false, reported: true, migration: string(MigrationMigrated), delivery: string(DeliveryFailed), want: ReconciliationDeliveryFailed, readyBlock: true},
		{name: "rejected mismatch stays blocked", broken: true, reported: true, migration: string(MigrationMigrated), delivery: string(DeliveryDelivered), review: string(ReviewRejected), want: ReconciliationMismatchPendingReview, readyBlock: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := ReconcileReceipt(test.broken, test.legacy, test.reported, test.migration, test.delivery, test.review)
			if got != test.want {
				t.Fatalf("status got %s want %s", got, test.want)
			}
			if got.ReadyBlocked() != test.readyBlock {
				t.Fatalf("readyBlocked for %s got %v want %v", got, got.ReadyBlocked(), test.readyBlock)
			}
		})
	}
}
