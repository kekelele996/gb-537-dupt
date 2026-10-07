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

func newReceiptTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.TrustAnchor{}, &model.CertificateChain{}, &model.DependentService{}, &model.RolloverScenario{}, &model.Receipt{}, &model.AuditLog{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func receiptTestActor(role string, team string) util.Actor {
	return util.Actor{UserID: 3, Username: "owner", DisplayName: "Service Owner", Team: team, Role: role}
}

// setupReceiptScenario creates a scenario with one affected service (SVC-PAY,
// trust refs [1]) and one unaffected service (SVC-ORDER, trust refs [1,2]).
func setupReceiptScenario(t *testing.T, db *gorm.DB) (model.RolloverScenario, []model.DependentService) {
	t.Helper()
	now := time.Date(2032, 4, 2, 8, 0, 0, 0, time.UTC)
	anchors := []model.TrustAnchor{
		{AnchorCode: "TEST-OLD", SubjectDN: "CN=old", SerialNumber: "1", FingerprintSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", NotBefore: now.Add(-24 * time.Hour), NotAfter: now.Add(365 * 24 * time.Hour), KeyAlgorithm: "ECDSA", CertificateState: "valid", PemRedacted: "public certificate", CreatedAt: now, UpdatedAt: now},
		{AnchorCode: "TEST-NEW", SubjectDN: "CN=new", SerialNumber: "2", FingerprintSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", NotBefore: now.Add(-24 * time.Hour), NotAfter: now.Add(365 * 24 * time.Hour), KeyAlgorithm: "ECDSA", CertificateState: "valid", PemRedacted: "public certificate", CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&anchors).Error; err != nil {
		t.Fatal(err)
	}
	chains := []model.CertificateChain{
		{ChainCode: "CHAIN-OLD", TrustAnchorID: anchors[0].ID, LeafSubject: "CN=leaf", CertificateRefsJSON: "[]", ChainFingerprint: "fp1", ValidFrom: now.Add(-24 * time.Hour), ValidTo: now.Add(365 * 24 * time.Hour), ValidationResult: `{"valid":true}`, ChainState: "validated", SourceChecksum: "cs1", CreatedAt: now, UpdatedAt: now},
		{ChainCode: "CHAIN-NEXT", TrustAnchorID: anchors[1].ID, LeafSubject: "CN=leaf", CertificateRefsJSON: "[]", ChainFingerprint: "fp2", ValidFrom: now.Add(-24 * time.Hour), ValidTo: now.Add(365 * 24 * time.Hour), ValidationResult: `{"valid":true}`, ChainState: "validated", SourceChecksum: "cs2", CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&chains).Error; err != nil {
		t.Fatal(err)
	}
	services := []model.DependentService{
		{ServiceCode: "SVC-PAY", Name: "Payments", OwnerTeam: "Payments", Environment: "production", ChainID: chains[0].ID, ClientTrustRefsJSON: `[1]`, Protocol: "mtls", Criticality: "critical", DependencyEdgesJSON: "[]", ServiceState: "active", CreatedAt: now, UpdatedAt: now},
		{ServiceCode: "SVC-ORDER", Name: "Orders", OwnerTeam: "Payments", Environment: "production", ChainID: chains[0].ID, ClientTrustRefsJSON: `[1,2]`, Protocol: "mtls", Criticality: "high", DependencyEdgesJSON: "[]", ServiceState: "active", CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&services).Error; err != nil {
		t.Fatal(err)
	}
	snapshot := algorithm.NewSnapshot(
		algorithm.ScenarioConfig{Name: "receipt-test", OldAnchorID: anchors[0].ID, NewAnchorID: anchors[1].ID, OverlapStart: now.Add(time.Hour), OverlapEnd: now.Add(2 * time.Hour), CandidateChainIDs: []uint{chains[0].ID, chains[1].ID}, SimulationTime: now.Add(90 * time.Minute)},
		[]algorithm.AnchorSnapshot{{ID: anchors[0].ID, Code: "OLD", State: "valid", NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour)}, {ID: anchors[1].ID, Code: "NEW", State: "valid", NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour)}},
		[]algorithm.ChainSnapshot{{ID: chains[0].ID, Code: "CHAIN-OLD", AnchorID: anchors[0].ID, LeafSubject: "CN=leaf", ValidFrom: now.Add(-time.Hour), ValidTo: now.Add(24 * time.Hour), State: "validated", ValidationValid: true}, {ID: chains[1].ID, Code: "CHAIN-NEXT", AnchorID: anchors[1].ID, LeafSubject: "CN=leaf", ValidFrom: now.Add(-time.Hour), ValidTo: now.Add(24 * time.Hour), State: "validated", ValidationValid: true}},
		[]algorithm.ServiceSnapshot{{ID: services[0].ID, Code: "SVC-PAY", ChainID: chains[0].ID, TrustAnchorIDs: []uint{1}, Criticality: "critical", State: "active"}, {ID: services[1].ID, Code: "SVC-ORDER", ChainID: chains[0].ID, TrustAnchorIDs: []uint{1, 2}, Criticality: "high", State: "active"}},
	)
	result, err := algorithm.Simulate(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	snapshotJSON, _ := snapshot.Canonical()
	hash, _ := snapshot.Hash()
	affectedJSON, _ := encode(result.AffectedServices)
	pathsJSON, _ := encode(result.BrokenPaths)
	evidenceJSON, _ := encode(result.Evidence)
	scenario := model.RolloverScenario{Name: "receipt-test", OldAnchorID: anchors[0].ID, NewAnchorID: anchors[1].ID, OverlapStart: now.Add(time.Hour), OverlapEnd: now.Add(2 * time.Hour), CandidateChainIDs: `[1,2]`, AlgorithmVersion: algorithm.Version, InputHash: hash, InputSnapshot: snapshotJSON, SimulationTime: now.Add(90 * time.Minute), AffectedServicesJSON: affectedJSON, BrokenPathsJSON: pathsJSON, PathEvidenceJSON: evidenceJSON, ScenarioState: string(constants.ScenarioSimulated), Explanation: result.Explanation, CreatedBy: 2, CreatedByName: "operator", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&scenario).Error; err != nil {
		t.Fatal(err)
	}
	return scenario, services
}

func TestReceiptBackfillCreatesPendingReceipts(t *testing.T) {
	db := newReceiptTestDB(t)
	scenario, _ := setupReceiptScenario(t, db)
	svc := NewReceiptService(repository.NewReceiptRepository(db), repository.NewRolloverScenarioRepository(db), repository.NewDependentServiceRepository(db), repository.NewAuditRepository(db), repository.NewTransactionManager(db))
	actor := receiptTestActor(string(constants.RolePKIOperator), "PKI Platform")
	result, err := svc.Backfill(context.Background(), scenario.ID, actor, "req-backfill")
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.TotalExpected != 1 {
		t.Fatalf("expected 1 expected receipt, got %d", result.Summary.TotalExpected)
	}
	if result.Summary.PendingCount != 1 {
		t.Fatalf("expected 1 pending, got %d", result.Summary.PendingCount)
	}
	if result.Summary.Ready {
		t.Fatal("scenario should not be ready with pending receipts")
	}
}

func TestReceiptSubmitFailsWhenTrustSetExcludesNewRoot(t *testing.T) {
	db := newReceiptTestDB(t)
	scenario, services := setupReceiptScenario(t, db)
	svc := NewReceiptService(repository.NewReceiptRepository(db), repository.NewRolloverScenarioRepository(db), repository.NewDependentServiceRepository(db), repository.NewAuditRepository(db), repository.NewTransactionManager(db))
	actor := receiptTestActor(string(constants.RoleServiceOwner), "Payments")
	_, err := svc.Backfill(context.Background(), scenario.ID, actor, "req-backfill")
	if err != nil {
		t.Fatal(err)
	}
	// SVC-PAY has trust refs [1] only; claiming switched=true should fail
	receipt, err := svc.Submit(context.Background(), scenario.ID, services[0].ID, dto.SubmitReceiptRequest{SwitchedToNewRoot: true}, actor, "req-submit")
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ReceiptState != string(constants.ReceiptFailed) {
		t.Fatalf("expected failed state, got %s", receipt.ReceiptState)
	}
	if receipt.FailureReason == "" {
		t.Fatal("expected failure reason")
	}
}

func TestReceiptSubmitSuspendsWhenSimulationPredictsBreak(t *testing.T) {
	db := newReceiptTestDB(t)
	scenario, services := setupReceiptScenario(t, db)
	svc := NewReceiptService(repository.NewReceiptRepository(db), repository.NewRolloverScenarioRepository(db), repository.NewDependentServiceRepository(db), repository.NewAuditRepository(db), repository.NewTransactionManager(db))
	actor := receiptTestActor(string(constants.RoleServiceOwner), "Payments")
	_, err := svc.Backfill(context.Background(), scenario.ID, actor, "req-backfill")
	if err != nil {
		t.Fatal(err)
	}
	// Update SVC-PAY trust refs to include new root (simulation predicted break, but service has since switched)
	if err := db.Model(&services[0]).Update("client_trust_refs_json", `[1,2]`).Error; err != nil {
		t.Fatal(err)
	}
	// Submit switched=true should suspend (simulation predicts break, but trust refs now include new root)
	receipt, err := svc.Submit(context.Background(), scenario.ID, services[0].ID, dto.SubmitReceiptRequest{SwitchedToNewRoot: true}, actor, "req-submit")
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ReceiptState != string(constants.ReceiptSuspended) {
		t.Fatalf("expected suspended state, got %s", receipt.ReceiptState)
	}
}

func TestReceiptSubmitNotSwitchedBlocksReady(t *testing.T) {
	db := newReceiptTestDB(t)
	scenario, services := setupReceiptScenario(t, db)
	svc := NewReceiptService(repository.NewReceiptRepository(db), repository.NewRolloverScenarioRepository(db), repository.NewDependentServiceRepository(db), repository.NewAuditRepository(db), repository.NewTransactionManager(db))
	actor := receiptTestActor(string(constants.RoleServiceOwner), "Payments")
	_, err := svc.Backfill(context.Background(), scenario.ID, actor, "req-backfill")
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := svc.Submit(context.Background(), scenario.ID, services[0].ID, dto.SubmitReceiptRequest{SwitchedToNewRoot: false}, actor, "req-submit")
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ReceiptState != string(constants.ReceiptSubmitted) {
		t.Fatalf("expected submitted state, got %s", receipt.ReceiptState)
	}
	if receipt.ReconciliationStatus != constants.ReconciliationBlocked {
		t.Fatalf("expected blocked status, got %s", receipt.ReconciliationStatus)
	}
}

func TestReceiptRetryAfterTrustSetUpdate(t *testing.T) {
	db := newReceiptTestDB(t)
	scenario, services := setupReceiptScenario(t, db)
	svc := NewReceiptService(repository.NewReceiptRepository(db), repository.NewRolloverScenarioRepository(db), repository.NewDependentServiceRepository(db), repository.NewAuditRepository(db), repository.NewTransactionManager(db))
	actor := receiptTestActor(string(constants.RoleServiceOwner), "Payments")
	_, err := svc.Backfill(context.Background(), scenario.ID, actor, "req-backfill")
	if err != nil {
		t.Fatal(err)
	}
	// First submit fails because trust set excludes new root
	_, err = svc.Submit(context.Background(), scenario.ID, services[0].ID, dto.SubmitReceiptRequest{SwitchedToNewRoot: true}, actor, "req-submit")
	if err != nil {
		t.Fatal(err)
	}
	// Update the service trust set to include new root
	if err := db.Model(&services[0]).Update("client_trust_refs_json", `[1,2]`).Error; err != nil {
		t.Fatal(err)
	}
	// Retry should now suspend (simulation predicts break but trust set now includes new root)
	receipt, err := svc.Retry(context.Background(), scenario.ID, services[0].ID, dto.SubmitReceiptRequest{SwitchedToNewRoot: true}, actor, "req-retry")
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ReceiptState != string(constants.ReceiptSuspended) {
		t.Fatalf("expected suspended state after retry, got %s", receipt.ReceiptState)
	}
}

func TestReceiptReviewConfirm(t *testing.T) {
	db := newReceiptTestDB(t)
	scenario, services := setupReceiptScenario(t, db)
	svc := NewReceiptService(repository.NewReceiptRepository(db), repository.NewRolloverScenarioRepository(db), repository.NewDependentServiceRepository(db), repository.NewAuditRepository(db), repository.NewTransactionManager(db))
	owner := receiptTestActor(string(constants.RoleServiceOwner), "Payments")
	reviewer := receiptTestActor(string(constants.RoleSecurityReviewer), "Security")
	_, err := svc.Backfill(context.Background(), scenario.ID, owner, "req-backfill")
	if err != nil {
		t.Fatal(err)
	}
	// Update trust refs and submit suspended receipt
	if err := db.Model(&services[0]).Update("client_trust_refs_json", `[1,2]`).Error; err != nil {
		t.Fatal(err)
	}
	_, err = svc.Submit(context.Background(), scenario.ID, services[0].ID, dto.SubmitReceiptRequest{SwitchedToNewRoot: true}, owner, "req-submit")
	if err != nil {
		t.Fatal(err)
	}
	// Reviewer confirms
	confirmed, err := svc.Review(context.Background(), scenario.ID, services[0].ID, dto.ReviewReceiptRequest{Decision: "confirm", Comment: "verified"}, reviewer, "req-review")
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.ReceiptState != string(constants.ReceiptConfirmed) {
		t.Fatalf("expected confirmed state, got %s", confirmed.ReceiptState)
	}
	if confirmed.ReconciliationStatus != constants.ReconciliationOK {
		t.Fatalf("expected ok status, got %s", confirmed.ReconciliationStatus)
	}
}

func TestReceiptReviewReject(t *testing.T) {
	db := newReceiptTestDB(t)
	scenario, services := setupReceiptScenario(t, db)
	svc := NewReceiptService(repository.NewReceiptRepository(db), repository.NewRolloverScenarioRepository(db), repository.NewDependentServiceRepository(db), repository.NewAuditRepository(db), repository.NewTransactionManager(db))
	owner := receiptTestActor(string(constants.RoleServiceOwner), "Payments")
	reviewer := receiptTestActor(string(constants.RoleSecurityReviewer), "Security")
	_, err := svc.Backfill(context.Background(), scenario.ID, owner, "req-backfill")
	if err != nil {
		t.Fatal(err)
	}
	// Update trust refs and submit suspended receipt
	if err := db.Model(&services[0]).Update("client_trust_refs_json", `[1,2]`).Error; err != nil {
		t.Fatal(err)
	}
	_, err = svc.Submit(context.Background(), scenario.ID, services[0].ID, dto.SubmitReceiptRequest{SwitchedToNewRoot: true}, owner, "req-submit")
	if err != nil {
		t.Fatal(err)
	}
	// Reviewer rejects
	rejected, err := svc.Review(context.Background(), scenario.ID, services[0].ID, dto.ReviewReceiptRequest{Decision: "reject", Comment: "not verified"}, reviewer, "req-review")
	if err != nil {
		t.Fatal(err)
	}
	if rejected.ReceiptState != string(constants.ReceiptRejected) {
		t.Fatalf("expected rejected state, got %s", rejected.ReceiptState)
	}
	if rejected.ReconciliationStatus != constants.ReconciliationBlocked {
		t.Fatalf("expected blocked status, got %s", rejected.ReconciliationStatus)
	}
}

func TestReadyGateBlocksTransitionWithUnresolvedReceipts(t *testing.T) {
	db := newReceiptTestDB(t)
	scenario, services := setupReceiptScenario(t, db)
	receiptSvc := NewReceiptService(repository.NewReceiptRepository(db), repository.NewRolloverScenarioRepository(db), repository.NewDependentServiceRepository(db), repository.NewAuditRepository(db), repository.NewTransactionManager(db))
	scenarioSvc := NewRolloverScenarioService(repository.NewRolloverScenarioRepository(db), repository.NewTrustAnchorRepository(db), repository.NewCertificateChainRepository(db), repository.NewDependentServiceRepository(db), repository.NewReceiptRepository(db), repository.NewAuditRepository(db), repository.NewTransactionManager(db))
	owner := receiptTestActor(string(constants.RoleServiceOwner), "Payments")
	operator := receiptTestActor(string(constants.RolePKIOperator), "PKI Platform")
	_, err := receiptSvc.Backfill(context.Background(), scenario.ID, owner, "req-backfill")
	if err != nil {
		t.Fatal(err)
	}
	// Submit one receipt as not switched
	_, err = receiptSvc.Submit(context.Background(), scenario.ID, services[0].ID, dto.SubmitReceiptRequest{SwitchedToNewRoot: false}, owner, "req-submit")
	if err != nil {
		t.Fatal(err)
	}
	// Try to transition to ready — should fail
	_, err = scenarioSvc.Transition(context.Background(), scenario.ID, dto.RolloverScenarioTransitionRequest{ToState: string(constants.ScenarioReady)}, operator, "req-transition")
	var apiErr *util.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict || apiErr.Code != util.CodeReceiptNotReady {
		t.Fatalf("got %#v, want 409 %s", err, util.CodeReceiptNotReady)
	}
}

func TestReadyGateAllowsTransitionWhenAllReceiptsOK(t *testing.T) {
	db := newReceiptTestDB(t)
	scenario, services := setupReceiptScenario(t, db)
	receiptSvc := NewReceiptService(repository.NewReceiptRepository(db), repository.NewRolloverScenarioRepository(db), repository.NewDependentServiceRepository(db), repository.NewAuditRepository(db), repository.NewTransactionManager(db))
	scenarioSvc := NewRolloverScenarioService(repository.NewRolloverScenarioRepository(db), repository.NewTrustAnchorRepository(db), repository.NewCertificateChainRepository(db), repository.NewDependentServiceRepository(db), repository.NewReceiptRepository(db), repository.NewAuditRepository(db), repository.NewTransactionManager(db))
	owner := receiptTestActor(string(constants.RoleServiceOwner), "Payments")
	reviewer := receiptTestActor(string(constants.RoleSecurityReviewer), "Security")
	operator := receiptTestActor(string(constants.RolePKIOperator), "PKI Platform")
	_, err := receiptSvc.Backfill(context.Background(), scenario.ID, owner, "req-backfill")
	if err != nil {
		t.Fatal(err)
	}
	// Update SVC-PAY trust refs to include new root, submit switched → suspended, reviewer confirms
	if err := db.Model(&services[0]).Update("client_trust_refs_json", `[1,2]`).Error; err != nil {
		t.Fatal(err)
	}
	_, err = receiptSvc.Submit(context.Background(), scenario.ID, services[0].ID, dto.SubmitReceiptRequest{SwitchedToNewRoot: true}, owner, "req-submit")
	if err != nil {
		t.Fatal(err)
	}
	_, err = receiptSvc.Review(context.Background(), scenario.ID, services[0].ID, dto.ReviewReceiptRequest{Decision: "confirm"}, reviewer, "req-review")
	if err != nil {
		t.Fatal(err)
	}
	// Now transition to ready — should succeed
	result, err := scenarioSvc.Transition(context.Background(), scenario.ID, dto.RolloverScenarioTransitionRequest{ToState: string(constants.ScenarioReady)}, operator, "req-transition")
	if err != nil {
		t.Fatal(err)
	}
	if result.ScenarioState != string(constants.ScenarioReady) {
		t.Fatalf("expected ready state, got %s", result.ScenarioState)
	}
}
