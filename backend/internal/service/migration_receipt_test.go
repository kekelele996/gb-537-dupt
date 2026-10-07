package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"pki-certificate-rollover-impact/backend/internal/algorithm"
	"pki-certificate-rollover-impact/backend/internal/constants"
	"pki-certificate-rollover-impact/backend/internal/dto"
	"pki-certificate-rollover-impact/backend/internal/model"
	"pki-certificate-rollover-impact/backend/internal/repository"
	"pki-certificate-rollover-impact/backend/internal/util"
)

type receiptFixture struct {
	db        *gorm.DB
	service   *RolloverScenarioService
	scenario  model.RolloverScenario
	serviceA  model.DependentService
	serviceB  model.DependentService
	brokenID  uint
	healthyID uint
	newAnchor model.TrustAnchor
	owner     util.Actor
	reviewer  util.Actor
}

func newReceiptTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.TrustAnchor{}, &model.CertificateChain{}, &model.DependentService{}, &model.RolloverScenario{}, &model.MigrationReceipt{}, &model.AuditLog{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func setupReceiptFixture(t *testing.T) receiptFixture {
	t.Helper()
	db := newReceiptTestDB(t)
	now := time.Date(2032, 5, 1, 9, 0, 0, 0, time.UTC)
	anchors := []model.TrustAnchor{
		{AnchorCode: "OLD-ROOT", SubjectDN: "CN=old", SerialNumber: "1", FingerprintSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", NotBefore: now.Add(-time.Hour), NotAfter: now.Add(365 * 24 * time.Hour), KeyAlgorithm: "ECDSA", CertificateState: "valid", PemRedacted: "pub", CreatedAt: now, UpdatedAt: now},
		{AnchorCode: "NEW-ROOT", SubjectDN: "CN=new", SerialNumber: "2", FingerprintSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", NotBefore: now.Add(-time.Hour), NotAfter: now.Add(365 * 24 * time.Hour), KeyAlgorithm: "ECDSA", CertificateState: "valid", PemRedacted: "pub", CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&anchors).Error; err != nil {
		t.Fatal(err)
	}
	healthy := model.DependentService{ServiceCode: "HEALTHY", Name: "Healthy", OwnerTeam: "Payments", Environment: "production", ChainID: 1, ClientTrustRefsJSON: "[]", Protocol: "mtls", Criticality: "high", DependencyEdgesJSON: "[]", ServiceState: "active", CreatedAt: now, UpdatedAt: now}
	broken := model.DependentService{ServiceCode: "BROKEN", Name: "Broken", OwnerTeam: "Payments", Environment: "production", ChainID: 1, ClientTrustRefsJSON: "[]", Protocol: "mtls", Criticality: "critical", DependencyEdgesJSON: "[]", ServiceState: "active", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&healthy).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&broken).Error; err != nil {
		t.Fatal(err)
	}
	affected := []algorithm.AffectedService{{ServiceID: broken.ID, ServiceCode: broken.ServiceCode, Criticality: broken.Criticality, At: now, Reason: "trust set does not include new anchor"}}
	affectedJSON, _ := encode(affected)
	scenario := model.RolloverScenario{Name: "receipt workflow", OldAnchorID: anchors[0].ID, NewAnchorID: anchors[1].ID, OverlapStart: now.Add(time.Hour), OverlapEnd: now.Add(2 * time.Hour), CandidateChainIDs: "[]", AlgorithmVersion: algorithm.Version, InputHash: "receipt-hash", InputSnapshot: "{}", SimulationTime: now.Add(90 * time.Minute), AffectedServicesJSON: affectedJSON, BrokenPathsJSON: "[]", PathEvidenceJSON: "[]", ScenarioState: "simulated", Explanation: "one broken service", CreatedBy: 7, CreatedByName: "operator", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&scenario).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewRolloverScenarioService(
		repository.NewRolloverScenarioRepository(db),
		repository.NewTrustAnchorRepository(db),
		repository.NewCertificateChainRepository(db),
		repository.NewDependentServiceRepository(db),
		repository.NewAuditRepository(db),
		repository.NewTransactionManager(db),
	)
	owner := util.Actor{UserID: 9, Username: "owner", Role: string(constants.RoleServiceOwner), Team: "Payments"}
	reviewer := util.Actor{UserID: 11, Username: "reviewer", Role: string(constants.RoleSecurityReviewer)}
	return receiptFixture{db: db, service: svc, scenario: scenario, serviceA: healthy, serviceB: broken, brokenID: broken.ID, healthyID: healthy.ID, newAnchor: anchors[1], owner: owner, reviewer: reviewer}
}

func transitionToReadyExpectBlock(t *testing.T, f receiptFixture) {
	t.Helper()
	_, err := f.service.Transition(context.Background(), f.scenario.ID, dto.RolloverScenarioTransitionRequest{ToState: "ready"}, util.Actor{UserID: 7, Username: "operator", Role: string(constants.RolePKIOperator)}, "req-ready")
	var apiErr *util.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict || apiErr.Code != util.CodeStateTransition {
		t.Fatalf("got %#v, want 409 ready block", err)
	}
}

func TestOutstandingReceiptsBlockReadyThenCleared(t *testing.T) {
	f := setupReceiptFixture(t)
	transitionToReadyExpectBlock(t, f)
	if _, err := f.service.SubmitReceipt(context.Background(), f.scenario.ID, dto.SubmitMigrationReceiptRequest{ServiceID: f.healthyID, MigrationState: "migrated", TrustAnchorIDs: []uint{f.newAnchor.ID}}, f.owner, "req-healthy"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.SubmitReceipt(context.Background(), f.scenario.ID, dto.SubmitMigrationReceiptRequest{ServiceID: f.brokenID, MigrationState: "in_progress", TrustAnchorIDs: []uint{f.newAnchor.ID}}, f.owner, "req-progress"); err != nil {
		t.Fatal(err)
	}
	rec, err := f.service.Reconciliation(context.Background(), f.scenario.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.ReadyToMark || len(rec.Blocking) != 1 || rec.Blocking[0].ServiceID != f.brokenID {
		t.Fatalf("in_progress broken service must stay outstanding: %+v", rec.Blocking)
	}
	transitionToReadyExpectBlock(t, f)
}

func TestBrokenButClaimedMigratedHeldUntilIndependentReview(t *testing.T) {
	f := setupReceiptFixture(t)
	receipt, err := f.service.SubmitReceipt(context.Background(), f.scenario.ID, dto.SubmitMigrationReceiptRequest{ServiceID: f.brokenID, MigrationState: "migrated", TrustAnchorIDs: []uint{f.newAnchor.ID}}, f.owner, "req-claim")
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ReviewStatus != "pending" {
		t.Fatalf("contradictory claim must be held, got %s", receipt.ReviewStatus)
	}
	rec, _ := f.service.Reconciliation(context.Background(), f.scenario.ID)
	if rec.Counts["mismatch_pending_review"] != 1 || rec.ReadyToMark {
		t.Fatalf("expected one held mismatch blocking ready: %+v", rec.Counts)
	}
	transitionToReadyExpectBlock(t, f)
	// The owning service owner cannot resolve the hold themselves.
	if _, err := f.service.ReviewReceipt(context.Background(), f.scenario.ID, f.brokenID, dto.ReviewMigrationReceiptRequest{Decision: "cleared"}, f.owner, "req-self-review"); err == nil {
		t.Fatal("service owner must not clear their own held mismatch")
	}
	cleared, err := f.service.ReviewReceipt(context.Background(), f.scenario.ID, f.brokenID, dto.ReviewMigrationReceiptRequest{Decision: "cleared", Comment: "verified new anchor present"}, f.reviewer, "req-clear")
	if err != nil {
		t.Fatal(err)
	}
	if cleared.ReviewStatus != "cleared" {
		t.Fatalf("want cleared, got %s", cleared.ReviewStatus)
	}
	rec, _ = f.service.Reconciliation(context.Background(), f.scenario.ID)
	if rec.Counts["mismatch_pending_review"] != 0 || rec.Counts["reconciled"] != 1 {
		t.Fatalf("cleared mismatch should reconcile: %+v", rec.Counts)
	}
}

func TestFailedDeliveryRetriedIndependently(t *testing.T) {
	f := setupReceiptFixture(t)
	failed, err := f.service.SubmitReceipt(context.Background(), f.scenario.ID, dto.SubmitMigrationReceiptRequest{ServiceID: f.brokenID, MigrationState: "in_progress", Note: "SIMULATE_DELIVERY_FAILURE"}, f.owner, "req-fail")
	if err != nil {
		t.Fatal(err)
	}
	if failed.DeliveryState != "failed" || failed.DeliveryAttempts != 1 {
		t.Fatalf("want failed delivery, got %s attempts %d", failed.DeliveryState, failed.DeliveryAttempts)
	}
	// Another service reports successfully and is not blocked by the failure.
	other, err := f.service.SubmitReceipt(context.Background(), f.scenario.ID, dto.SubmitMigrationReceiptRequest{ServiceID: f.healthyID, MigrationState: "migrated", TrustAnchorIDs: []uint{f.newAnchor.ID}}, f.owner, "req-other")
	if err != nil {
		t.Fatal(err)
	}
	if other.DeliveryState != "delivered" {
		t.Fatalf("other service delivery should succeed independently: %s", other.DeliveryState)
	}
	rec, _ := f.service.Reconciliation(context.Background(), f.scenario.ID)
	if rec.Counts["delivery_failed"] != 1 {
		t.Fatalf("want one delivery_failed, got %+v", rec.Counts)
	}
	transitionToReadyExpectBlock(t, f)
	// Retrying a delivered receipt is rejected.
	if _, err := f.service.RetryReceipt(context.Background(), f.scenario.ID, f.healthyID, dto.RetryMigrationReceiptRequest{}, f.owner, "req-retry-ok"); err == nil {
		t.Fatal("only failed receipts may be retried")
	}
	retried, err := f.service.RetryReceipt(context.Background(), f.scenario.ID, f.brokenID, dto.RetryMigrationReceiptRequest{Note: "resent without marker"}, f.owner, "req-retry")
	if err != nil {
		t.Fatal(err)
	}
	if retried.DeliveryState != "delivered" || retried.DeliveryAttempts != 2 {
		t.Fatalf("retry should deliver, got %s attempts %d", retried.DeliveryState, retried.DeliveryAttempts)
	}
}

func TestLegacyBackfillOnlyAddsMissingAndListsSeparately(t *testing.T) {
	f := setupReceiptFixture(t)
	// One service already has a real receipt before the upgrade backfill.
	if _, err := f.service.SubmitReceipt(context.Background(), f.scenario.ID, dto.SubmitMigrationReceiptRequest{ServiceID: f.healthyID, MigrationState: "migrated", TrustAnchorIDs: []uint{f.newAnchor.ID}}, f.owner, "req-existing"); err != nil {
		t.Fatal(err)
	}
	rec, err := f.service.BackfillReceipts(context.Background(), f.scenario.ID, util.Actor{UserID: 7, Username: "operator", Role: string(constants.RolePKIOperator)}, "req-backfill")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Counts["legacy_missing"] != 1 || rec.Counts["reconciled"] != 1 {
		t.Fatalf("backfill should add only the missing service: %+v", rec.Counts)
	}
	// Running backfill again must not duplicate or overwrite.
	again, err := f.service.BackfillReceipts(context.Background(), f.scenario.ID, util.Actor{UserID: 7, Username: "operator", Role: string(constants.RolePKIOperator)}, "req-backfill-2")
	if err != nil {
		t.Fatal(err)
	}
	if again.Counts["legacy_missing"] != 1 || again.TotalServices != 2 {
		t.Fatalf("idempotent backfill changed state: %+v", again.Counts)
	}
	// The owner fills the legacy placeholder in; it becomes a real receipt.
	filled, err := f.service.SubmitReceipt(context.Background(), f.scenario.ID, dto.SubmitMigrationReceiptRequest{ServiceID: f.brokenID, MigrationState: "migrated", TrustAnchorIDs: []uint{f.newAnchor.ID}}, f.owner, "req-fill-legacy")
	if err != nil {
		t.Fatal(err)
	}
	if filled.LegacyBackfilled {
		t.Fatal("resubmitting a legacy placeholder must clear the legacy marker")
	}
	// This service was predicted broken and now claims migrated without a review, so it is held.
	if filled.ReviewStatus != "pending" {
		t.Fatalf("filled legacy broken service claiming migrated must be held, got %s", filled.ReviewStatus)
	}
}
